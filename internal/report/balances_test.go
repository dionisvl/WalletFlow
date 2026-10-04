package report

import (
	"testing"

	"github.com/dionisvl/walletflow/internal/ledger"
)

func TestBuildBalances(t *testing.T) {
	book := ledger.Book{
		"a": {Address: "a", Name: "A", Kind: ledger.KindMine},
		"b": {Address: "b", Name: "B", Kind: ledger.KindMine},
	}
	const day = 86400
	trx := ledger.Asset{Chain: "tron", Symbol: "TRX", Decimals: 6}
	ts := []ledger.Transfer{
		{TS: 0, Chain: "tron", Asset: trx, From: "x", To: "a", AmountRaw: "10000000", FeeRaw: "0"},
		{TS: day, Chain: "tron", Asset: trx, From: "a", To: "b", AmountRaw: "4000000", FeeRaw: "1000000"},
		{TS: 2 * day, Chain: "tron", Asset: trx, From: "b", To: "y", AmountRaw: "1000000", FeeRaw: "0"},
	}
	got := BuildBalances(ts, book)
	want := map[string][]float64{"A": {10, 5, 5}, "B": {0, 4, 3}}
	for _, s := range got.Series {
		for i, v := range want[s.Name] {
			if s.Values[i] != v {
				t.Errorf("%s day %d = %v, want %v", s.Name, i, s.Values[i], v)
			}
		}
	}
}
