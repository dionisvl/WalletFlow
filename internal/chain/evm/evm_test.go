package evm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"walletflow/internal/chain"
	"walletflow/internal/ledger"
)

func TestParseErrors(t *testing.T) {
	tests := []struct {
		body     string
		retry    bool
		noAccess bool
		wantErr  bool
	}{
		{`{"status":"0","message":"No transactions found","result":[]}`, false, false, false},
		{`{"status":"0","message":"NOTOK","result":"Max rate limit reached"}`, true, false, true},
		{`{"status":"0","message":"NOTOK","result":"Free API access is not supported for this chain. Please upgrade your api plan for full chain coverage."}`, false, true, true},
		{`{"status":"0","message":"NOTOK","result":"Missing/Invalid API Key"}`, false, false, true},
	}
	for _, tt := range tests {
		var r response
		if err := json.Unmarshal([]byte(tt.body), &r); err != nil {
			t.Fatal(err)
		}
		_, retry, err := parse(r)
		if retry != tt.retry || (err != nil) != tt.wantErr || errors.Is(err, ErrNoChainAccess) != tt.noAccess {
			t.Errorf("%s: retry=%v err=%v", tt.body, retry, err)
		}
	}
}

func TestPullFixtures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		name := "testdata/etherscan_empty.json"
		if q.Get("startblock") == "0" {
			switch q.Get("action") {
			case "txlist":
				name = "testdata/etherscan_txlist.json"
			case "tokentx":
				name = "testdata/etherscan_tokentx.json"
			}
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(b)
	}))
	defer srv.Close()

	c := &Client{Key: "k", BaseURL: srv.URL, HTTP: srv.Client()}
	eth, _ := chain.ByKey("ethereum")
	cur := &chain.MemCursors{}
	var got []ledger.Transfer
	emit := chain.Collect(&got)
	mine := "0x2222222222222222222222222222222222222222"
	if n, err := c.Pull(context.Background(), eth, mine, cur, emit, func(string) {}); err != nil || n != 4 {
		t.Fatalf("Pull = %d, %v", n, err)
	}
	// Second run starts from the saved block and finds nothing new.
	if n, err := c.Pull(context.Background(), eth, mine, cur, emit, func(string) {}); err != nil || n != 0 {
		t.Fatalf("second Pull = %d, %v", n, err)
	}
	want := map[string]struct{ amount, symbol, fee string }{
		"0xaaa1": {"1", "ETH", "210000000000000"},
		"0xaaa2": {"0.5", "ETH", "210000000000000"},
		"0xaaa3": {"0", "ETH", "21000"}, // failed tx keeps only the fee
		"0xbbb1": {"2.5", "USDT", "0"},
	}
	for _, tr := range got {
		w, ok := want[tr.TxHash]
		if !ok {
			t.Errorf("unexpected %s", tr.TxHash)
			continue
		}
		if tr.Amount() != w.amount || tr.Asset.Symbol != w.symbol || tr.FeeRaw != w.fee {
			t.Errorf("%s = %s %s fee %s, want %+v", tr.TxHash, tr.Amount(), tr.Asset.Symbol, tr.FeeRaw, w)
		}
	}
}
