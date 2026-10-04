package tron

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dionisvl/WalletFlow/internal/chain"
	"github.com/dionisvl/WalletFlow/internal/ledger"
)

const owner = "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm"

func TestPullFixtures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "testdata/tron_tx.json"
		if strings.HasSuffix(r.URL.Path, "/trc20") {
			name = "testdata/tron_trc20.json"
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	var got []ledger.Transfer
	n, err := c.Pull(context.Background(), owner, &chain.MemCursors{}, chain.Collect(&got), func(string) {})
	if err != nil || n != len(got) || n == 0 {
		t.Fatalf("Pull = %d, %v (collected %d)", n, err, len(got))
	}
	byHash := map[string]ledger.Transfer{}
	for _, tr := range got {
		byHash[tr.TxHash] = tr
	}
	trx := byHash["b6d699f0b9cead74f36677ac983baa86868f77c7715bad7ee11393cef27b891f"]
	if trx.Amount() != "50" || trx.Asset.Symbol != "TRX" || trx.FeeRaw != "1100000" || !strings.HasPrefix(trx.From, "T") {
		t.Errorf("TRX transfer = %+v", trx)
	}
	usdt := byHash["f75bc10665d5cc0acf3c251f1209cbf20771e509f17c85e1b7801abc9c4a2c85"]
	if usdt.Amount() != "416666666" || usdt.Asset.Symbol != "USDT" || usdt.To != owner {
		t.Errorf("USDT transfer = %+v", usdt)
	}
}

// TestLive hits the real TronGrid. Run with WF_LIVE=1.
func TestLive(t *testing.T) {
	if os.Getenv("WF_LIVE") == "" {
		t.Skip("set WF_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c := &Client{Key: os.Getenv("TRONGRID_API_KEY"), HTTP: &http.Client{Timeout: 30 * time.Second}, Limit: chain.NewLimiter(time.Second)}
	var got []ledger.Transfer
	n, err := c.Pull(ctx, owner, &chain.MemCursors{}, chain.Collect(&got), func(m string) { t.Log(m) })
	t.Logf("added %d, err %v", n, err)
	if n == 0 {
		t.Fatal("nothing synced")
	}
}

// TestLivePerDay prints activity of real addresses. Run with WF_LIVE=1.
func TestLivePerDay(t *testing.T) {
	if os.Getenv("WF_LIVE") == "" {
		t.Skip("set WF_LIVE=1")
	}
	c := &Client{Key: os.Getenv("TRONGRID_API_KEY"), HTTP: &http.Client{Timeout: 30 * time.Second}, Limit: chain.NewLimiter(300 * time.Millisecond)}
	for _, a := range strings.Fields(os.Getenv("WF_ADDRS")) {
		r, err := c.PerDay(context.Background(), a)
		t.Logf("%s: %.1f/day (%.0f/year) err=%v", a, r, r*365, err)
	}
}
