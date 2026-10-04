// Package syncer runs background syncs of all own addresses.
package syncer

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"walletflow/internal/address"
	"walletflow/internal/chain"
	"walletflow/internal/chain/evm"
	"walletflow/internal/chain/tron"
	"walletflow/internal/ledger"
	"walletflow/internal/store"
)

// Status is what the UI shows about the current or last run.
type Status struct {
	Running  bool
	Step     int
	Total    int
	Message  string
	Added    int
	Errors   []string
	Finished time.Time
	Active   []string // what each worker is doing now

	active map[string]string
}

func (st *Status) setActive(key, msg string) {
	if st.active == nil {
		st.active = map[string]string{}
	}
	st.active[key] = msg
}

// Keys returns the API keys to use for a run.
type Keys func(ctx context.Context) (etherscan, trongrid string)

// Enabled returns chain keys the user wants synced.
type Enabled func(ctx context.Context) map[string]bool

// Syncer runs one sync at a time.
type Syncer struct {
	store   *store.Store
	chains  []chain.Chain
	keys    Keys
	enabled Enabled
	http    *http.Client

	mu      sync.Mutex
	status  Status
	cancel  context.CancelFunc
	blocked map[string]bool // chains the API plan does not allow, per run

	etherscanURL, tronURL string // empty = real APIs; tests point them at fakes

	// MaxPerAddress stops syncing an address once it has this many transfers; 0 = no limit.
	MaxPerAddress int
}

// New creates a syncer over the available chains.
func New(st *store.Store, chains []chain.Chain, keys Keys, enabled Enabled) *Syncer {
	return &Syncer{store: st, chains: chains, keys: keys, enabled: enabled,
		http: &http.Client{Timeout: 60 * time.Second}, MaxPerAddress: DefaultMaxPerAddress}
}

func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Errors = slices.Clone(s.status.Errors)
	st.active = nil
	for _, m := range s.status.active {
		st.Active = append(st.Active, m)
	}
	slices.Sort(st.Active)
	return st
}

func (s *Syncer) update(f func(*Status)) {
	s.mu.Lock()
	f(&s.status)
	s.mu.Unlock()
}

// Start launches a sync unless one is already running.
func (s *Syncer) Start(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running {
		return false
	}
	s.status = Status{Running: true, Message: "Старт…"}
	s.blocked = map[string]bool{}
	ctx, s.cancel = context.WithCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("sync panic: %v", r)
				s.update(func(st *Status) { st.Errors = append(st.Errors, fmt.Sprint("panic: ", r)) })
			}
			stopped := ctx.Err() != nil
			s.cancel()
			s.update(func(st *Status) {
				st.Running = false
				st.Finished = time.Now()
				st.Message = fmt.Sprintf("Готово: +%d трансферов", st.Added)
				if stopped {
					st.Message = fmt.Sprintf("Остановлено: +%d трансферов. Следующий синк продолжит с места остановки.", st.Added)
				}
			})
		}()
		s.run(ctx)
		// Classify what we have, even when stopped.
		s.update(func(st *Status) { st.Message = "Классификация…" })
		if err := s.store.RefreshBook(context.WithoutCancel(ctx)); err != nil {
			s.fail(err)
		}
	}()
	return true
}

// Stop cancels a running sync.
func (s *Syncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.Running && s.cancel != nil {
		s.cancel()
		s.status.Message = "Останавливаю…"
	}
}

// errRemoved stops a job whose address was removed or is no longer synced.
var errRemoved = errors.New("address removed from own wallets")

// errTooBig stops a job whose address has more transfers than the limit.
var errTooBig = errors.New("too many transfers")

// DefaultMaxPerAddress is the default cap on stored transfers per synced address.
// Personal wallets stay far below; exchange and service wallets go far above.
const DefaultMaxPerAddress = 10_000

type job struct {
	chain chain.Chain
	addr  string
	kind  ledger.Kind
}

func (s *Syncer) run(ctx context.Context) {
	addrs, err := s.store.Addresses(ctx)
	if err != nil {
		s.fail(err)
		return
	}
	enabled := s.enabled(ctx)
	var jobs []job
	for _, a := range addrs {
		if !a.Kind.Synced() {
			continue
		}
		for _, c := range s.chains {
			if c.Family == a.Family && enabled[c.Key] {
				jobs = append(jobs, job{c, a.Address, a.Kind})
			}
		}
	}
	s.update(func(st *Status) { st.Total = len(jobs) })

	ethKey, tronKey := s.keys(ctx)
	ec := &evm.Client{Key: ethKey, BaseURL: s.etherscanURL, HTTP: s.http, Limit: chain.NewLimiter(400 * time.Millisecond)}
	tc := &tron.Client{Key: tronKey, BaseURL: s.tronURL, HTTP: s.http, Limit: chain.NewLimiter(250 * time.Millisecond)}
	if tronKey == "" {
		tc.Limit = chain.NewLimiter(time.Second) // public limits are tight
	}
	if ethKey == "" && slices.ContainsFunc(jobs, func(j job) bool { return j.chain.Family == address.EVM }) {
		s.fail(errors.New("нет ключа Etherscan (ETHERSCAN_API_KEY в .env или Настройки), EVM сети пропущены"))
	}

	// A few addresses at a time, so a huge history does not hold up small ones.
	// The per-provider limiters keep the request rate within API limits.
	queue := make(chan job)
	var wg sync.WaitGroup
	for range min(workers, len(jobs)) {
		wg.Go(func() {
			for j := range queue {
				s.runJob(ctx, j, ec, tc)
			}
		})
	}
	for _, j := range jobs {
		select {
		case queue <- j:
		case <-ctx.Done():
		}
	}
	close(queue)
	wg.Wait()
}

// workers is how many addresses sync at once.
const workers = 4

func (s *Syncer) runJob(ctx context.Context, j job, ec *evm.Client, tc *tron.Client) {
	if ctx.Err() != nil {
		return
	}
	key := j.chain.Name + " " + address.Short(j.addr)
	defer func() {
		if r := recover(); r != nil {
			s.fail(fmt.Errorf("%s: panic: %v", key, r))
		}
		s.update(func(st *Status) {
			st.Step++
			delete(st.active, key)
		})
	}()
	if j.chain.Family == address.EVM && (ec.Key == "" || s.isBlocked(j.chain.Key)) {
		return
	}
	progress := func(msg string) { s.update(func(st *Status) { st.setActive(key, msg) }) }
	progress(key + ": старт")
	have, err := s.store.CountTransfers(ctx, store.Filter{Addresses: []string{j.addr}})
	if err != nil {
		s.fail(fmt.Errorf("%s: %w", key, err))
		return
	}
	if s.MaxPerAddress > 0 && have >= s.MaxPerAddress {
		s.failTooBig(key, have)
		return
	}
	// Checked before each page: deleting an address mid-sync stops its download,
	// and an address that grows past the limit stops too.
	emit := func(ctx context.Context, ts []ledger.Transfer) (int, error) {
		if ok, err := s.store.IsSynced(ctx, j.addr); err != nil || !ok {
			return 0, cmp.Or(err, errRemoved)
		}
		if s.MaxPerAddress > 0 && have >= s.MaxPerAddress {
			return 0, errTooBig
		}
		n, err := s.store.InsertTransfers(ctx, ts)
		have += n
		s.update(func(st *Status) { st.Added += n })
		return n, err
	}
	switch j.chain.Family {
	case address.EVM:
		_, err = ec.Pull(ctx, j.chain, j.addr, s.store, emit, progress)
		if errors.Is(err, evm.ErrNoChainAccess) {
			if s.block(j.chain.Key) {
				s.fail(fmt.Errorf("%s: недоступна на бесплатном тарифе Etherscan. Добавь %q в IGNORE_CHAINS в .env или сними галочку в Настройках", j.chain.Name, j.chain.Key))
			}
			return
		}
	case address.Tron:
		_, err = tc.Pull(ctx, j.addr, s.store, emit, progress)
	}
	switch {
	case errors.Is(err, errRemoved):
		return
	case errors.Is(err, errTooBig):
		s.failTooBig(key, have)
		return
	}
	if err != nil && ctx.Err() == nil {
		s.fail(fmt.Errorf("%s: %w", key, err))
	}
}

func (s *Syncer) failTooBig(key string, n int) {
	s.fail(fmt.Errorf("%s: синк остановлен на %d трансферах, похоже на биржу или сервис. "+
		"Если это биржа, смени тип на «Биржа»: её история не нужна, лишнее удалится. "+
		"Лимит: MAX_TRANSFERS_PER_ADDRESS в .env (0 = без лимита)", key, n))
}

// block marks a chain unavailable on the API plan; it reports whether it was newly blocked.
func (s *Syncer) block(chain string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked[chain] {
		return false
	}
	s.blocked[chain] = true
	return true
}

func (s *Syncer) isBlocked(chain string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.blocked[chain]
}

func (s *Syncer) fail(err error) {
	log.Printf("sync: %v", err)
	s.update(func(st *Status) { st.Errors = append(st.Errors, err.Error()) })
}
