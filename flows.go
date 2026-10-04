package main

import (
	"cmp"
	"slices"
)

type sankeyNode struct {
	Name  string `json:"name"`  // unique id: side + ":" + label
	Label string `json:"label"` // what is shown
	Color string `json:"color,omitempty"`
}

type sankeyLink struct {
	Source string  `json:"source"`
	Target string  `json:"target"`
	Value  float64 `json:"value"`
	Count  int     `json:"count"`
	Wallet string  `json:"wallet"` // my side, for drill-down
	Class  string  `json:"class"`
}

type sankey struct {
	Nodes  []sankeyNode `json:"nodes"`
	Links  []sankeyLink `json:"links"`
	Symbol string       `json:"symbol"`
	In     float64      `json:"in"`
	Out    float64      `json:"out"`
}

const otherLabel = "Чужие (без разметки)"

// buildSankey lays flows out in three columns: sources → my wallets → destinations.
// Internal transfers are skipped: they would create cycles and are not real flows.
func buildSankey(ts []Transfer, book map[string]Address, minPct float64) sankey {
	nameOf := func(addr, category string) string {
		if a, ok := book[addr]; ok && a.Name != "" {
			return a.Name
		}
		if _, ok := book[addr]; !ok && category != "" {
			return category
		}
		if a, ok := book[addr]; ok && a.Kind == KindMine {
			return shortAddr(addr)
		}
		return otherLabel
	}
	type key struct{ src, dst string }
	links := map[key]*sankeyLink{}
	var out sankey
	for _, t := range ts {
		if t.AmountRaw == "0" {
			continue
		}
		v := amountFloat(t.AmountRaw, t.Decimals)
		out.Symbol = t.Symbol
		var src, dst, wallet string
		switch t.Class {
		case ClassInflow, ClassCEXWithdrawal:
			wallet = nameOf(t.To, "")
			src, dst = "in:"+nameOf(t.From, t.Category), "me:"+wallet
			out.In += v
		case ClassOutflow, ClassCEXDeposit:
			wallet = nameOf(t.From, "")
			src, dst = "me:"+wallet, "out:"+nameOf(t.To, t.Category)
			out.Out += v
		default:
			continue
		}
		k := key{src, dst}
		l := links[k]
		if l == nil {
			l = &sankeyLink{Source: src, Target: dst, Wallet: wallet, Class: string(t.Class)}
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
	slices.SortFunc(out.Links, func(a, b sankeyLink) int { return cmp.Compare(b.Value, a.Value) })

	colors := map[string]string{}
	for _, a := range book {
		if a.Name != "" && a.Color != "" {
			colors[a.Name] = a.Color
		}
	}
	for n := range used {
		label := trimSide(n)
		out.Nodes = append(out.Nodes, sankeyNode{Name: n, Label: label, Color: colors[label]})
	}
	slices.SortFunc(out.Nodes, func(a, b sankeyNode) int { return cmp.Compare(a.Name, b.Name) })
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
