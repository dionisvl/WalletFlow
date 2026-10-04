package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestLiveTron hits the real TronGrid. Run with WF_LIVE=1.
func TestLiveTron(t *testing.T) {
	if os.Getenv("WF_LIVE") == "" {
		t.Skip("set WF_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	st := testStore(t)
	g := &TronGrid{HTTP: &http.Client{Timeout: 30 * time.Second}, Limit: newLimiter(time.Second)}
	n, err := syncTron(ctx, st, g, "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm", func(m string) { t.Log(m) })
	t.Logf("added %d, err %v", n, err)
	if n == 0 {
		t.Fatal("nothing synced")
	}
}
