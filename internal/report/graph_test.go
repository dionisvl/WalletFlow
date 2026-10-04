package report

import (
	"testing"

	"walletflow/internal/ledger"
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

	g := BuildGraph(known, unknown, book, GraphOptions{Group: true, MaxUnknown: 1})
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

	g = BuildGraph(known, nil, book, GraphOptions{})
	if len(g.Nodes) != 4 {
		t.Errorf("ungrouped nodes = %d, want 4", len(g.Nodes))
	}
}
