package main

import (
	"math/big"
	"slices"
	"time"
)

type balanceSeries struct {
	Name   string    `json:"name"`
	Color  string    `json:"color,omitempty"`
	Values []float64 `json:"values"`
}

type balances struct {
	Days   []string        `json:"days"`
	Series []balanceSeries `json:"series"`
	Symbol string          `json:"symbol"`
}

// buildBalances replays transfers of one asset into daily balances per own wallet.
// Fees count only when the asset is the chain's native coin.
func buildBalances(ts []Transfer, book map[string]Address) balances {
	var out balances
	if len(ts) == 0 {
		return out
	}
	nameOf := func(addr string) (string, bool) {
		a, ok := book[addr]
		if !ok || a.Kind != KindMine {
			return "", false
		}
		return a.Label(), true
	}
	const day = 24 * 60 * 60
	delta := map[string]map[int64]*big.Int{} // wallet → day → change
	add := func(name string, d int64, v *big.Int) {
		m := delta[name]
		if m == nil {
			m = map[int64]*big.Int{}
			delta[name] = m
		}
		if m[d] == nil {
			m[d] = new(big.Int)
		}
		m[d].Add(m[d], v)
	}
	first, last := ts[0].TS/day, ts[0].TS/day
	decimals := ts[0].Decimals
	out.Symbol = ts[0].Symbol
	for _, t := range ts {
		d := t.TS / day
		first, last = min(first, d), max(last, d)
		amt, ok := new(big.Int).SetString(t.AmountRaw, 10)
		if !ok {
			continue
		}
		if name, ok := nameOf(t.From); ok {
			spent := new(big.Int).Set(amt)
			if c, _ := chainByKey(t.Chain); c.Symbol == t.Symbol && t.FeeRaw != "0" {
				if fee, ok := new(big.Int).SetString(t.FeeRaw, 10); ok {
					spent.Add(spent, fee)
				}
			}
			add(name, d, spent.Neg(spent))
		}
		if name, ok := nameOf(t.To); ok {
			add(name, d, amt)
		}
	}
	last = max(last, time.Now().Unix()/day)
	for d := first; d <= last; d++ {
		out.Days = append(out.Days, time.Unix(d*day, 0).UTC().Format("2006-01-02"))
	}
	names := make([]string, 0, len(delta))
	for n := range delta {
		names = append(names, n)
	}
	slices.Sort(names)
	colors := map[string]string{}
	for _, a := range book {
		if a.Color != "" {
			colors[a.Label()] = a.Color
		}
	}
	for _, n := range names {
		s := balanceSeries{Name: n, Color: colors[n], Values: make([]float64, 0, len(out.Days))}
		bal := new(big.Int)
		for d := first; d <= last; d++ {
			if v := delta[n][d]; v != nil {
				bal.Add(bal, v)
			}
			s.Values = append(s.Values, amountFloat(bal.String(), decimals))
		}
		out.Series = append(out.Series, s)
	}
	return out
}
