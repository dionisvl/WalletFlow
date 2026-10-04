package report

import (
	"testing"

	"github.com/dionisvl/WalletFlow/internal/ledger"
)

func TestBuildGraph(t *testing.T) {
	book := ledger.Book{
		"m1": {Address: "m1", Name: "Ledger", Kind: ledger.KindMine},
		"m2": {Address: "m2", Name: "Ledger", Kind: ledger.KindMine},
		"m3": {Address: "m3", Name: "Hot", Kind: ledger.KindMine},
		"cx": {Address: "cx", Name: "Binance", Kind: ledger.KindExchange},
	}
	usdt := ledger.Asset{Chain: "tron", Contract: "T1", Symbol: "USDT", Decimals: 6}
	tr := func(from, to string, c ledger.Class, amount string, ts int64) ledger.Transfer {
		return ledger.Transfer{From: from, To: to, Class: c, AmountRaw: amount, Asset: usdt, TS: ts}
	}
	known := []ledger.Transfer{
		tr("cx", "m1", ledger.ClassCEXWithdrawal, "1000000", 1),
		tr("cx", "m2", ledger.ClassCEXWithdrawal, "2000000", 2),
		tr("m1", "m2", ledger.ClassInternal, "5", 3), // same entity, skipped when grouped
		tr("m2", "m3", ledger.ClassInternal, "500000", 4),
	}
	unknown := []ledger.Transfer{
		tr("u1", "m3", ledger.ClassInflow, "1", 5),
		tr("u1", "m3", ledger.ClassInflow, "1", 6),
		tr("u2", "m3", ledger.ClassInflow, "1", 7),
		tr("u3", "m3", ledger.ClassInflow, "1", 8),
	}

	all := append(append([]ledger.Transfer{}, known...), unknown...)
	g := BuildGraph(all, book, GraphOptions{Group: true, MaxUnknown: 1, UnknownIn: true})
	edges := map[string]GraphEdge{}
	for _, e := range g.Edges {
		edges[e.Source+">"+e.Target] = e
	}
	if e := edges["n:Binance>n:Ledger"]; e.Count != 2 || e.Totals[0].Amount != "3" || e.Class != "cex_withdrawal" {
		t.Errorf("binance→ledger = %+v", e)
	}
	if _, ok := edges["n:Ledger>n:Ledger"]; ok {
		t.Error("self loop kept")
	}
	if e := edges["u:u1>n:Hot"]; e.Count != 2 || !e.Unknown {
		t.Errorf("top unknown = %+v", e)
	}
	if e := edges["more:in>n:Hot"]; e.Count != 2 || g.HiddenUnknown != 2 {
		t.Errorf("folded = %+v hidden %d", e, g.HiddenUnknown)
	}

	g = BuildGraph(all, book, GraphOptions{})
	if len(g.Nodes) != 4 {
		t.Errorf("ungrouped nodes = %d, want 4", len(g.Nodes))
	}
}

// Two watched wallets never paid each other but share a counterparty.
func TestBuildGraphCommon(t *testing.T) {
	book := ledger.Book{
		"w1": {Address: "w1", Name: "A", Kind: ledger.KindWatch},
		"w2": {Address: "w2", Name: "B", Kind: ledger.KindWatch},
	}
	trx := ledger.Asset{Chain: "tron", Symbol: "TRX", Decimals: 6}
	tr := func(from, to string, ts int64) ledger.Transfer {
		return ledger.Transfer{From: from, To: to, Class: ledger.ClassUnknown, AmountRaw: "1000000", Asset: trx, TS: ts}
	}
	ts := []ledger.Transfer{
		tr("w1", "hub", 1), tr("hub", "w2", 2), // hub links A and B
		tr("w1", "lonely", 3), // only A
	}
	g := BuildGraph(ts, book, GraphOptions{Group: true, Common: true, UnknownSince: 1 << 40})
	kinds := map[string]string{}
	for _, n := range g.Nodes {
		kinds[n.ID] = n.Kind
	}
	if kinds["u:hub"] != "shared" || kinds["u:lonely"] != "" || len(g.Edges) != 2 {
		t.Errorf("nodes %v, %d edges", kinds, len(g.Edges))
	}
	// Outside window and without Common nothing but the two wallets' nodes is there.
	g = BuildGraph(ts, book, GraphOptions{Group: true, UnknownIn: true, UnknownOut: true, UnknownSince: 1 << 40})
	if len(g.Nodes) != 2 || len(g.Edges) != 0 {
		t.Errorf("nodes = %d edges %d, want the 2 wallets alone", len(g.Nodes), len(g.Edges))
	}
}
