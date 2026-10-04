package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter allows one request per interval.
type limiter struct {
	mu    sync.Mutex
	every time.Duration
	next  time.Time
}

func newLimiter(every time.Duration) *limiter { return &limiter{every: every} }

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	at := l.next
	if at.Before(now) {
		at = now
	}
	l.next = at.Add(l.every)
	l.mu.Unlock()
	if d := time.Until(at); d > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d):
		}
	}
	return nil
}

// SyncStatus is what the UI shows about the current or last run.
type SyncStatus struct {
	Running  bool
	Step     int
	Total    int
	Message  string
	Added    int
	Errors   []string
	Finished time.Time
}

// Syncer runs one sync at a time in the background.
type Syncer struct {
	store *Store
	http  *http.Client

	mu     sync.Mutex
	status SyncStatus
	cancel context.CancelFunc
}

func newSyncer(st *Store) *Syncer {
	return &Syncer{store: st, http: &http.Client{Timeout: 60 * time.Second}}
}

func (s *Syncer) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	st.Errors = append([]string(nil), s.status.Errors...)
	return st
}

func (s *Syncer) update(f func(*SyncStatus)) {
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
	s.status = SyncStatus{Running: true, Message: "Старт…"}
	ctx, s.cancel = context.WithCancel(ctx)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("sync panic: %v", r)
				s.update(func(st *SyncStatus) { st.Errors = append(st.Errors, fmt.Sprint("panic: ", r)) })
			}
			stopped := ctx.Err() != nil
			s.cancel()
			s.update(func(st *SyncStatus) {
				st.Running = false
				st.Finished = time.Now()
				st.Message = fmt.Sprintf("Готово: +%d трансферов", st.Added)
				if stopped {
					st.Message = fmt.Sprintf("Остановлено: +%d трансферов. Следующий синк продолжит с места остановки.", st.Added)
				}
			})
		}()
		s.run(ctx)
		// Classify what we have even when stopped.
		s.update(func(ss *SyncStatus) { ss.Message = "Классификация…" })
		if err := s.store.Reclassify(context.WithoutCancel(ctx)); err != nil {
			s.fail(err)
		}
	}()
	return true
}

type syncJob struct {
	chain Chain
	addr  string
}

func (s *Syncer) run(ctx context.Context) {
	st := s.store
	addrs, err := st.Addresses(ctx)
	if err != nil {
		s.fail(err)
		return
	}
	enabled := enabledChains(ctx, st)
	var jobs []syncJob
	for _, a := range addrs {
		if a.Kind != KindMine {
			continue
		}
		for _, c := range chains {
			if c.Family == a.Family && enabled[c.Key] {
				jobs = append(jobs, syncJob{c, a.Address})
			}
		}
	}
	s.update(func(ss *SyncStatus) { ss.Total = len(jobs) })

	es := &Etherscan{Key: st.Setting(ctx, "etherscan_key", ""), HTTP: s.http, Limit: newLimiter(400 * time.Millisecond)}
	tg := &TronGrid{Key: st.Setting(ctx, "trongrid_key", ""), HTTP: s.http, Limit: newLimiter(250 * time.Millisecond)}
	if es.Key == "" && hasFamily(jobs, FamilyEVM) {
		s.fail(errors.New("нет ключа Etherscan (Настройки), EVM пропущены"))
	}
	if tg.Key == "" {
		tg.Limit = newLimiter(time.Second) // public limits are tight
	}

	blocked := map[string]bool{} // chains the API plan does not allow
	for i, j := range jobs {
		if ctx.Err() != nil {
			return
		}
		s.update(func(ss *SyncStatus) { ss.Step = i + 1 })
		if j.chain.Family == FamilyEVM && (es.Key == "" || blocked[j.chain.Key]) {
			continue
		}
		progress := func(msg string) { s.update(func(ss *SyncStatus) { ss.Message = msg }) }
		var n int
		var err error
		switch j.chain.Family {
		case FamilyEVM:
			n, err = syncEVM(ctx, st, es, j.chain, j.addr, progress)
			if errors.Is(err, errNoChainAccess) {
				blocked[j.chain.Key] = true
			}
		case FamilyTron:
			n, err = syncTron(ctx, st, tg, j.addr, progress)
		}
		s.update(func(ss *SyncStatus) { ss.Added += n })
		if err != nil && ctx.Err() == nil {
			s.fail(fmt.Errorf("%s %s: %w", j.chain.Name, shortAddr(j.addr), err))
		}
	}
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

func (s *Syncer) fail(err error) {
	log.Printf("sync: %v", err)
	s.update(func(ss *SyncStatus) { ss.Errors = append(ss.Errors, err.Error()) })
}

func hasFamily(jobs []syncJob, f string) bool {
	for _, j := range jobs {
		if j.chain.Family == f {
			return true
		}
	}
	return false
}

// defaultChains are enabled until the user picks their own.
const defaultChains = "ethereum,arbitrum,base,optimism,polygon,bsc,tron"

func enabledChains(ctx context.Context, st *Store) map[string]bool {
	m := map[string]bool{}
	for k := range strings.SplitSeq(st.Setting(ctx, "chains", defaultChains), ",") {
		if k != "" {
			m[k] = true
		}
	}
	return m
}
