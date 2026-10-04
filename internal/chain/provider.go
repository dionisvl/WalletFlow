package chain

import (
	"context"
	"sync"
	"time"

	"walletflow/internal/ledger"
)

// Limiter allows one request per interval. The zero value does not limit.
type Limiter struct {
	mu    sync.Mutex
	every time.Duration
	next  time.Time
}

func NewLimiter(every time.Duration) *Limiter { return &Limiter{every: every} }

// Wait blocks until the next request is allowed.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
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

// Cursors remembers where the last sync of a stream stopped.
type Cursors interface {
	Cursor(ctx context.Context, chain, addr, stream string) int64
	SetCursor(ctx context.Context, chain, addr, stream string, cursor int64) error
}

// Emit saves a batch of transfers and returns how many were new.
type Emit func(ctx context.Context, ts []ledger.Transfer) (int, error)

// Backoff waits before retry attempt n (n >= 1).
func Backoff(ctx context.Context, n int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(time.Duration(n) * 2 * time.Second):
		return nil
	}
}
