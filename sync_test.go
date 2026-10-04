package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := openStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func serveFile(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseEtherscanErrors(t *testing.T) {
	tests := []struct {
		body      string
		retry     bool
		noAccess  bool
		wantError bool
	}{
		{`{"status":"0","message":"No transactions found","result":[]}`, false, false, false},
		{`{"status":"0","message":"NOTOK","result":"Max rate limit reached"}`, true, false, true},
		{`{"status":"0","message":"NOTOK","result":"Free API access is not supported for this chain. Please upgrade your api plan for full chain coverage."}`, false, true, true},
		{`{"status":"0","message":"NOTOK","result":"Missing/Invalid API Key"}`, false, false, true},
	}
	for _, tt := range tests {
		var r etherscanResp
		if err := jsonUnmarshal(tt.body, &r); err != nil {
			t.Fatal(err)
		}
		_, retry, err := parseEtherscan(r)
		if retry != tt.retry || (err != nil) != tt.wantError || isNoAccess(err) != tt.noAccess {
			t.Errorf("%s: retry=%v err=%v", tt.body, retry, err)
		}
	}
}

func TestSyncEndToEnd(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	mine := "0x2222222222222222222222222222222222222222"
	tronMine := "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm"
	for _, a := range []Address{
		{Family: FamilyEVM, Address: mine, Name: "Main", Kind: KindMine},
		{Family: FamilyEVM, Address: "0x1111111111111111111111111111111111111111", Name: "Binance", Kind: KindExchange},
		{Family: FamilyTron, Address: tronMine, Name: "Tron", Kind: KindMine},
	} {
		if err := st.UpsertAddress(ctx, a); err != nil {
			t.Fatal(err)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case strings.HasSuffix(r.URL.Path, "/transactions/trc20"):
			w.Write(serveFile(t, "tron_trc20.json"))
		case strings.HasSuffix(r.URL.Path, "/transactions"):
			w.Write(serveFile(t, "tron_tx.json"))
		case q.Get("chainid") != "1" || q.Get("startblock") != "0":
			w.Write(serveFile(t, "etherscan_empty.json"))
		case q.Get("action") == "txlist":
			w.Write(serveFile(t, "etherscan_txlist.json"))
		case q.Get("action") == "tokentx":
			w.Write(serveFile(t, "etherscan_tokentx.json"))
		default:
			w.Write(serveFile(t, "etherscan_empty.json"))
		}
	}))
	defer srv.Close()

	es := &Etherscan{Key: "k", BaseURL: srv.URL, HTTP: srv.Client(), Limit: newLimiter(time.Millisecond)}
	tg := &TronGrid{BaseURL: srv.URL, HTTP: srv.Client(), Limit: newLimiter(time.Millisecond)}
	eth, _ := chainByKey("ethereum")
	n, err := syncEVM(ctx, st, es, eth, mine, func(string) {})
	if err != nil || n != 4 {
		t.Fatalf("syncEVM = %d, %v", n, err)
	}
	// Second run is incremental and idempotent.
	if n, err := syncEVM(ctx, st, es, eth, mine, func(string) {}); err != nil || n != 0 {
		t.Fatalf("second syncEVM = %d, %v", n, err)
	}
	if _, err := syncTron(ctx, st, tg, tronMine, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if err := st.Reclassify(ctx); err != nil {
		t.Fatal(err)
	}

	all, err := st.Transfers(ctx, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	byHash := map[string]Transfer{}
	for _, tr := range all {
		byHash[tr.TxHash] = tr
	}
	checks := []struct {
		hash   string
		class  Class
		amount string
	}{
		{"0xaaa1", ClassCEXWithdrawal, "1"},
		{"0xaaa2", ClassOutflow, "0.5"},
		{"0xaaa3", ClassOutflow, "0"}, // failed tx keeps only the fee
		{"0xbbb1", ClassInflow, "2.5"},
		{"f75bc10665d5cc0acf3c251f1209cbf20771e509f17c85e1b7801abc9c4a2c85", ClassInflow, "416666666"},
	}
	for _, c := range checks {
		tr, ok := byHash[c.hash]
		if !ok {
			t.Errorf("%s missing", c.hash)
			continue
		}
		if tr.Class != c.class || tr.Amount() != c.amount {
			t.Errorf("%s: class %s amount %s, want %s %s", c.hash, tr.Class, tr.Amount(), c.class, c.amount)
		}
	}
	if fee := byHash["0xaaa2"].FeeRaw; fee != "210000000000000" {
		t.Errorf("fee = %s", fee)
	}

	// Inbox skips zero amounts; a rule clears matching rows.
	before, _ := st.CountTransfers(ctx, Filter{Inbox: true})
	if err := st.AddRule(ctx, Rule{Counterparty: "0x9999999999999999999999999999999999999999", Class: ClassOutflow, Category: "Оплата"}); err != nil {
		t.Fatal(err)
	}
	after, _ := st.CountTransfers(ctx, Filter{Inbox: true})
	if after != before-1 {
		t.Errorf("inbox %d -> %d after rule", before, after)
	}
	if got, _ := st.Transfer(ctx, byHash["0xaaa2"].ID); got.Category != "Оплата" {
		t.Errorf("category = %q", got.Category)
	}

	// Marking a counterparty as mine turns the flow into an internal transfer.
	if err := st.UpsertAddress(ctx, Address{Family: FamilyEVM, Address: "0x7777777777777777777777777777777777777777", Kind: KindMine}); err != nil {
		t.Fatal(err)
	}
	if err := st.Reclassify(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Transfer(ctx, byHash["0xbbb1"].ID); got.Class != ClassInternal {
		t.Errorf("class after marking = %s", got.Class)
	}
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

func isNoAccess(err error) bool { return errors.Is(err, errNoChainAccess) }
