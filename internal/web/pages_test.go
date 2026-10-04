package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dionisvl/walletflow/internal/config"
	"github.com/dionisvl/walletflow/internal/ledger"
	"github.com/dionisvl/walletflow/internal/report"
	"github.com/dionisvl/walletflow/internal/store"
)

func testServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, a := range []ledger.Address{
		{Family: "tron", Address: "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm", Name: "Main", Kind: ledger.KindMine},
		{Family: "tron", Address: "TV6MuMXfmLbBqPZvBHdwFsDnQeVfnmiuSi", Name: "Binance", Kind: ledger.KindExchange},
	} {
		st.UpsertAddress(ctx, a)
	}
	usdt := ledger.Asset{Chain: "tron", Contract: "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", Symbol: "USDT", Decimals: 6}
	st.InsertTransfers(ctx, []ledger.Transfer{
		{UID: "1", Chain: "tron", TxHash: "h1", TS: 1700000000, From: "TV6MuMXfmLbBqPZvBHdwFsDnQeVfnmiuSi", To: "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm", Asset: usdt, AmountRaw: "5000000", FeeRaw: "0"},
		{UID: "2", Chain: "tron", TxHash: "h2", TS: 1700000100, From: "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm", To: "TAiK6ijSGs6TPNavfFK1W86iKeZf7otdAG", Asset: usdt, AmountRaw: "1000000", FeeRaw: "0"},
		{UID: "3", Chain: "tron", TxHash: "h3", TS: 1700000200, From: "TAiK6ijSGs6TPNavfFK1W86iKeZf7otdAG", To: "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm", Asset: usdt, AmountRaw: "2000000", FeeRaw: "0"},
	})
	st.Reclassify(ctx)
	s, err := New(ctx, st, config.Config{IgnoreChains: []string{"base"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, s.Handler()
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "127.0.0.1:8080"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPagesRender(t *testing.T) {
	_, h := testServer(t)
	for _, p := range []string{"/inbox", "/transactions", "/transactions?wallet=Main&src=Binance&dst=Main",
		"/flows", "/balances", "/graph", "/wallets", "/export", "/settings",
		"/flows/data?asset=1", "/balances/data?asset=1", "/graph/data?unknown_in=1&unknown_out=1&days=3650"} {
		w := get(t, h, p)
		if w.Code != 200 {
			t.Errorf("%s = %d: %s", p, w.Code, w.Body.String())
		}
	}
	if body := get(t, h, "/settings").Body.String(); strings.Contains(body, `value="base"`) {
		t.Error("ignored chain is offered in settings")
	}
	if body := get(t, h, "/inbox").Body.String(); !strings.Contains(body, "TAiK6i") {
		t.Error("inbox misses the external transfer")
	}
}

func TestGraphData(t *testing.T) {
	_, h := testServer(t)
	var g report.Graph
	decode := func(q url.Values) {
		g = report.Graph{}
		w := get(t, h, "/graph/data?"+q.Encode())
		if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
			t.Fatal(err, w.Body.String())
		}
	}
	decode(url.Values{})
	if len(g.Nodes) != 2 || len(g.Edges) != 1 {
		t.Errorf("known graph = %d nodes %d edges", len(g.Nodes), len(g.Edges))
	}
	// Old transfers fall outside the unknown window…
	decode(url.Values{"unknown_in": {"1"}, "days": {"30"}})
	if len(g.Nodes) != 2 {
		t.Errorf("unknown outside window shown: %d nodes", len(g.Nodes))
	}
	// …but show up with a window that covers them.
	decode(url.Values{"unknown_in": {"1"}, "unknown_out": {"1"}, "to": {"2023-11-15"}, "days": {"30"}})
	if len(g.Nodes) != 3 || len(g.Edges) != 3 {
		t.Errorf("with unknown = %d nodes %d edges", len(g.Nodes), len(g.Edges))
	}
}

func TestWalletsAddExportRoundTrip(t *testing.T) {
	s, h := testServer(t)
	post := func(form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/wallets", strings.NewReader(form.Encode()))
		r.Host = "127.0.0.1:8080"
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// Single add with the three fields, then straight to the graph.
	w := post(url.Values{"address": {"TAiK6ijSGs6TPNavfFK1W86iKeZf7otdAG"}, "name": {"Suspect"}, "kind": {"watch"}, "then": {"graph"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/graph?common=1" {
		t.Fatalf("add+graph = %d %s", w.Code, w.Header().Get("Location"))
	}
	s.sync.Stop()

	exp := get(t, h, "/wallets/export").Body.String()
	for _, want := range []string{"TAiK6ijSGs6TPNavfFK1W86iKeZf7otdAG, Suspect, watch", "TQrY8tryqsYVCYS3MFbtffiPp2ccyn4STm, Main, mine"} {
		if !strings.Contains(exp, want) {
			t.Errorf("export misses %q:\n%s", want, exp)
		}
	}
	// Import the export back into a fresh server: same book.
	s2, h2 := testServer(t)
	_ = s2
	r := httptest.NewRequest("POST", "/wallets", strings.NewReader(url.Values{"lines": {exp + "TXgjzMc3vRxqtZcGyHhjtQuPUts9MDZpNX, , биржа\n"}}.Encode()))
	r.Host = "127.0.0.1:8080"
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h2.ServeHTTP(httptest.NewRecorder(), r)
	book, _ := s2.store.Book(context.Background())
	if a := book["TAiK6ijSGs6TPNavfFK1W86iKeZf7otdAG"]; a.Kind != ledger.KindWatch || a.Name != "Suspect" {
		t.Errorf("round trip = %+v", a)
	}
	if a := book["TXgjzMc3vRxqtZcGyHhjtQuPUts9MDZpNX"]; a.Kind != ledger.KindExchange || a.Name != "" {
		t.Errorf("empty name line = %+v", a)
	}
}
