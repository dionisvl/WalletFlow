// Package syncer runs background syncs of all own addresses.
package syncer

import (
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

	mu     sync.Mutex
	status Status
	cancel context.CancelFunc
}

// New creates a syncer over the available chains.
func New(st *store.Store, chains []chain.Chain, keys Keys, enabled Enabled) *Syncer {
	return &Syncer{store: st, chains: chains, keys: keys, enabled: enabled,
		http: &http.Client{Timeout: 60 * time.Second}}
}

func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Errors = slices.Clone(s.status.Errors)
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
		if err := s.store.Reclassify(context.WithoutCancel(ctx)); err != nil {
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

type job struct {
	chain chain.Chain
	addr  string
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
		if a.Kind != ledger.KindMine {
			continue
		}
		for _, c := range s.chains {
			if c.Family == a.Family && enabled[c.Key] {
				jobs = append(jobs, job{c, a.Address})
			}
		}
	}
	s.update(func(st *Status) { st.Total = len(jobs) })

	ethKey, tronKey := s.keys(ctx)
	ec := &evm.Client{Key: ethKey, HTTP: s.http, Limit: chain.NewLimiter(400 * time.Millisecond)}
	tc := &tron.Client{Key: tronKey, HTTP: s.http, Limit: chain.NewLimiter(250 * time.Millisecond)}
	if tronKey == "" {
		tc.Limit = chain.NewLimiter(time.Second) // public limits are tight
	}
	if ethKey == "" && slices.ContainsFunc(jobs, func(j job) bool { return j.chain.Family == address.EVM }) {
		s.fail(errors.New("нет ключа Etherscan (ETHERSCAN_API_KEY в .env или Настройки), EVM сети пропущены"))
	}

	blocked := map[string]bool{} // chains the API plan does not allow
	for i, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		s.update(func(st *Status) { st.Step = i + 1 })
		if j.chain.Family == address.EVM && (ethKey == "" || blocked[j.chain.Key]) {
			continue
		}
		progress := func(msg string) { s.update(func(st *Status) { st.Message = msg }) }
		var n int
		var err error
		switch j.chain.Family {
		case address.EVM:
			n, err = ec.Pull(ctx, j.chain, j.addr, s.store, s.store.InsertTransfers, progress)
			if errors.Is(err, evm.ErrNoChainAccess) {
				blocked[j.chain.Key] = true
				err = fmt.Errorf("недоступна на бесплатном тарифе Etherscan. Добавь %q в IGNORE_CHAINS в .env или сними галочку в Настройках", j.chain.Key)
			}
		case address.Tron:
			n, err = tc.Pull(ctx, j.addr, s.store, s.store.InsertTransfers, progress)
		}
		s.update(func(st *Status) { st.Added += n })
		if err != nil && ctx.Err() == nil {
			if blocked[j.chain.Key] {
				s.fail(fmt.Errorf("%s: %w", j.chain.Name, err))
			} else {
				s.fail(fmt.Errorf("%s %s: %w", j.chain.Name, address.Short(j.addr), err))
			}
		}
	}
}

func (s *Syncer) fail(err error) {
	log.Printf("sync: %v", err)
	s.update(func(st *Status) { st.Errors = append(st.Errors, err.Error()) })
}
