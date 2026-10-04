package chain

import (
	"context"
	"sync"

	"walletflow/internal/ledger"
)

// MemCursors keeps cursors in memory. For tests and dry runs.
type MemCursors struct {
	mu sync.Mutex
	m  map[string]int64
}

func (c *MemCursors) Cursor(_ context.Context, chain, addr, stream string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[chain+"|"+addr+"|"+stream]
}

func (c *MemCursors) SetCursor(_ context.Context, chain, addr, stream string, cursor int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]int64{}
	}
	c.m[chain+"|"+addr+"|"+stream] = cursor
	return nil
}

// Collect is an Emit that appends transfers to *dst, deduplicating by UID.
func Collect(dst *[]ledger.Transfer) Emit {
	seen := map[string]bool{}
	return func(_ context.Context, ts []ledger.Transfer) (int, error) {
		n := 0
		for _, t := range ts {
			if !seen[t.UID] {
				seen[t.UID] = true
				*dst = append(*dst, t)
				n++
			}
		}
		return n, nil
	}
}
