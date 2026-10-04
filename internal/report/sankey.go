// Package report turns transfers into chart data: flows, balances and the graph.
package report

import (
	"cmp"
	"slices"

	"github.com/dionisvl/WalletFlow/internal/address"
	"github.com/dionisvl/WalletFlow/internal/ledger"
)

// SankeyNode is a node of the flow chart.
type SankeyNode struct {
	Name  string `json:"name"`  // unique id: side + ":" + label
	Label string `json:"label"` // what is shown
	Color string `json:"color,omitempty"`
}

// SankeyLink is an aggregated flow between two nodes.
type SankeyLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Value  float64 `json:"value"`
	Count  int     `json:"count"`
	Wallet string  `json:"wallet"` // my side, for drill-down
	Class  string  `json:"class"`
}

// Sankey is the flow chart data.
type Sankey struct {
	Nodes  []SankeyNode `json:"nodes"`
	Links  []SankeyLink `json:"links"`
	Symbol string       `json:"symbol"`
	In     float64      `json:"in"`
	Out    float64      `json:"out"`
}

// otherLabel names unnamed, unreviewed outside addresses; the UI translates it.
const otherLabel = "Unreviewed outsiders"

// BuildSankey lays flows out in three columns: sources → my wallets → destinations.
// Internal transfers are skipped: they would create cycles and are not real flows.
func BuildSankey(ts []ledger.Transfer, book ledger.Book, minPct float64) Sankey {
	nameOf := func(addr, category string) string {
		if a, ok := book[addr]; ok && a.Name != "" {
			return a.Name
		}
		if _, ok := book[addr]; !ok && category != "" {
			return category
		}
		if a, ok := book[addr]; ok && a.Kind == ledger.KindMine {
			return address.Short(addr)
		}
		return otherLabel
	}
	type key struct{ src, dst string }
	links := map[key]*SankeyLink{}
	var out Sankey
	for _, t := range ts {
		if t.AmountRaw == "0" {
			continue
		}
		v := ledger.AmountFloat(t.AmountRaw, t.Asset.Decimals)
		out.Symbol = t.Asset.Symbol
		var src, dst, wallet string
		switch t.Class {
		case ledger.ClassInflow, ledger.ClassCEXWithdrawal:
			wallet = nameOf(t.To, "")
			src, dst = "in:"+nameOf(t.From, t.Category), "me:"+wallet
			out.In += v
		case ledger.ClassOutflow, ledger.ClassCEXDeposit:
			wallet = nameOf(t.From, "")
			src, dst = "me:"+wallet, "out:"+nameOf(t.To, t.Category)
			out.Out += v
		default:
			continue
		}
		k := key{src, dst}
		l := links[k]
		if l == nil {
			l = &SankeyLink{Source: src, Target: dst, Wallet: wallet, Class: string(t.Class)}
			links[k] = l
		}
		l.Value += v
		l.Count++
	}

	total := out.In + out.Out
	used := map[string]bool{}
	for _, l := range links {
		if total > 0 && l.Value/total*100 < minPct {
			continue
		}
		out.Links = append(out.Links, *l)
		used[l.Source], used[l.Target] = true, true
	}
	slices.SortFunc(out.Links, func(a, b SankeyLink) int { return cmp.Compare(b.Value, a.Value) })

	colors := map[string]string{}
	for _, a := range book {
		if a.Name != "" && a.Color != "" {
			colors[a.Name] = a.Color
		}
	}
	for n := range used {
		label := trimSide(n)
		out.Nodes = append(out.Nodes, SankeyNode{Name: n, Label: label, Color: colors[label]})
	}
	slices.SortFunc(out.Nodes, func(a, b SankeyNode) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func trimSide(n string) string {
	for i := range len(n) {
		if n[i] == ':' {
			return n[i+1:]
		}
	}
	return n
}
