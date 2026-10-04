package main

import "testing"

func TestBuildBalances(t *testing.T) {
	book := map[string]Address{
		"a": {Address: "a", Name: "A", Kind: KindMine},
		"b": {Address: "b", Name: "B", Kind: KindMine},
	}
	const day = 86400
	ts := []Transfer{
		{TS: 0, Chain: "tron", Symbol: "TRX", Decimals: 6, From: "x", To: "a", AmountRaw: "10000000", FeeRaw: "0"},
		{TS: day, Chain: "tron", Symbol: "TRX", Decimals: 6, From: "a", To: "b", AmountRaw: "4000000", FeeRaw: "1000000"},
		{TS: 2 * day, Chain: "tron", Symbol: "TRX", Decimals: 6, From: "b", To: "y", AmountRaw: "1000000", FeeRaw: "0"},
	}
	got := buildBalances(ts, book)
	want := map[string][]float64{"A": {10, 5, 5}, "B": {0, 4, 3}}
	for _, s := range got.Series {
		for i, v := range want[s.Name] {
			if s.Values[i] != v {
				t.Errorf("%s day %d = %v, want %v", s.Name, i, s.Values[i], v)
			}
		}
	}
}

func TestLookalike(t *testing.T) {
	mine := "0x1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa5678"
	p := Page{Book: map[string]Address{mine: {Address: mine, Name: "Ledger", Kind: KindMine}}}
	if got := p.Lookalike("0x1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb5678"); got != "Ledger" {
		t.Errorf("poison not detected: %q", got)
	}
	if got := p.Lookalike(mine); got != "" {
		t.Errorf("own address flagged: %q", got)
	}
	if got := p.Lookalike("0x9999bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb5678"); got != "" {
		t.Errorf("false positive: %q", got)
	}
}
