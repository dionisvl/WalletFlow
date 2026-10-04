package syncer

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dionisvl/walletflow/internal/chain"
	"github.com/dionisvl/walletflow/internal/ledger"
	"github.com/dionisvl/walletflow/internal/store"
)

const (
	big   = "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm"
	small = "TV6MuMXfmLbBqPZvBHdwFsDnQeVfnmiuSi"
)

// fakeTron serves an endless history for big and one transfer for small.
func fakeTron(t *testing.T) *httptest.Server {
	var page atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/trc20") {
			fmt.Fprint(w, `{"success":true,"data":[]}`)
			return
		}
		if strings.Contains(r.URL.Path, small) {
			fmt.Fprint(w, `{"success":true,"data":[{"txID":"s1","block_timestamp":1000,"ret":[{"contractRet":"SUCCESS","fee":0}],
				"raw_data":{"contract":[{"type":"TransferContract","parameter":{"value":{"amount":5,
				"owner_address":"41a614f803b6fd780986a42c78ec9c7f77e6ded13c","to_address":"41d2f6a3e3ef4c2b8a0e1c5b2f0e6c3b1d4a5f6e7d"}}}]}}]}`)
			return
		}
		n := page.Add(1)
		fmt.Fprintf(w, `{"success":true,"data":[{"txID":"b%d","block_timestamp":%d,"ret":[{"contractRet":"SUCCESS","fee":0}],
			"raw_data":{"contract":[{"type":"TransferContract","parameter":{"value":{"amount":1,
			"owner_address":"41a614f803b6fd780986a42c78ec9c7f77e6ded13c","to_address":"41d2f6a3e3ef4c2b8a0e1c5b2f0e6c3b1d4a5f6e7d"}}}]}}],
			"meta":{"links":{"next":"http://%s%s?p=%d"}}}`, n, n, r.Host, r.URL.Path, n)
	}))
}

func TestSmallWalletDoesNotWaitForBig(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, a := range []string{big, small} { // big first: sequential sync would never reach small
		st.UpsertAddress(ctx, ledger.Address{Family: "tron", Address: a, Kind: ledger.KindWatch})
	}
	srv := fakeTron(t)
	defer srv.Close()

	tronChain, _ := chain.ByKey("tron")
	s := New(st, []chain.Chain{tronChain},
		func(context.Context) (string, string) { return "", "key" },
		func(context.Context) map[string]bool { return map[string]bool{"tron": true} })
	s.tronURL = srv.URL
	s.Start(ctx, nil)
	defer s.Stop()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		all, _ := st.Transfers(ctx, store.Filter{})
		if slices.ContainsFunc(all, func(tr ledger.Transfer) bool { return tr.TxHash == "s1" }) {
			if !s.Status().Running {
				t.Fatal("sync ended: big history should still be loading")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("small wallet was not synced while the big one was loading")
}

func TestStopsAtLimit(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.UpsertAddress(ctx, ledger.Address{Family: "tron", Address: big, Kind: ledger.KindWatch})
	srv := fakeTron(t)
	defer srv.Close()

	tronChain, _ := chain.ByKey("tron")
	s := New(st, []chain.Chain{tronChain},
		func(context.Context) (string, string) { return "", "key" },
		func(context.Context) map[string]bool { return map[string]bool{"tron": true} })
	s.tronURL = srv.URL
	s.MaxPerAddress = 5
	s.Start(ctx, nil)

	deadline := time.Now().Add(10 * time.Second)
	for s.Status().Running {
		if time.Now().After(deadline) {
			s.Stop()
			t.Fatal("sync did not stop at the limit")
		}
		time.Sleep(20 * time.Millisecond)
	}
	errs := s.Status().Errors
	if len(errs) != 1 || !strings.Contains(errs[0], "looks like an exchange") {
		t.Errorf("errors = %q", errs)
	}
}

// A wallet whose newest 200 transfers fit into an hour is skipped before any download.
func TestSkipsBusyAddress(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.UpsertAddress(ctx, ledger.Address{Family: "tron", Address: big, Kind: ledger.KindWatch})

	var ascRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("order_by"), "asc") {
			ascRequests.Add(1)
		}
		var b strings.Builder
		b.WriteString(`{"success":true,"data":[`)
		for i := range 200 {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"txID":"t%d","transaction_id":"t%d","block_timestamp":%d,"value":"1","raw_data":{"contract":[]}}`, i, i, 1_700_000_000_000+int64(i)*1000)
		}
		b.WriteString(`]}`)
		fmt.Fprint(w, b.String())
	}))
	defer srv.Close()

	tronChain, _ := chain.ByKey("tron")
	s := New(st, []chain.Chain{tronChain},
		func(context.Context) (string, string) { return "", "key" },
		func(context.Context) map[string]bool { return map[string]bool{"tron": true} })
	s.tronURL = srv.URL
	s.Start(ctx, nil)
	for deadline := time.Now().Add(5 * time.Second); s.Status().Running; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			s.Stop()
			t.Fatal("sync did not finish")
		}
	}
	errs := s.Status().Errors
	if len(errs) != 1 || !strings.Contains(errs[0], "skipped") {
		t.Errorf("errors = %q", errs)
	}
	if n := ascRequests.Load(); n != 0 {
		t.Errorf("history download requests = %d, want 0", n)
	}
}
