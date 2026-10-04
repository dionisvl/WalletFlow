package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/dionisvl/WalletFlow/internal/ledger"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestClassifyRulesInbox(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	const (
		mine  = "0x2222222222222222222222222222222222222222"
		cex   = "0x1111111111111111111111111111111111111111"
		shop  = "0x9999999999999999999999999999999999999999"
		payer = "0x7777777777777777777777777777777777777777"
	)
	for _, a := range []ledger.Address{
		{Family: "evm", Address: mine, Name: "Main", Kind: ledger.KindMine},
		{Family: "evm", Address: cex, Name: "Binance", Kind: ledger.KindExchange},
	} {
		if err := s.UpsertAddress(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	eth := ledger.Asset{Chain: "ethereum", Symbol: "ETH", Decimals: 18}
	usdt := ledger.Asset{Chain: "ethereum", Contract: "0xdac1", Symbol: "USDT", Decimals: 6}
	ts := []ledger.Transfer{
		{UID: "1", Chain: "ethereum", TxHash: "a1", TS: 1, From: cex, To: mine, Asset: eth, AmountRaw: "1000000000000000000", FeeRaw: "0"},
		{UID: "2", Chain: "ethereum", TxHash: "a2", TS: 2, From: mine, To: shop, Asset: eth, AmountRaw: "500000000000000000", FeeRaw: "21000"},
		{UID: "3", Chain: "ethereum", TxHash: "a3", TS: 3, From: mine, To: shop, Asset: eth, AmountRaw: "0", FeeRaw: "21000"},
		{UID: "4", Chain: "ethereum", TxHash: "b1", TS: 4, From: payer, To: mine, Asset: usdt, AmountRaw: "2500000", FeeRaw: "0"},
	}
	if n, err := s.InsertTransfers(ctx, ts); err != nil || n != 4 {
		t.Fatalf("insert = %d, %v", n, err)
	}
	if n, err := s.InsertTransfers(ctx, ts); err != nil || n != 0 {
		t.Fatalf("re-insert = %d, %v", n, err)
	}
	if err := s.Reclassify(ctx); err != nil {
		t.Fatal(err)
	}
	byHash := func() map[string]ledger.Transfer {
		all, err := s.Transfers(ctx, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]ledger.Transfer{}
		for _, tr := range all {
			m[tr.TxHash] = tr
		}
		return m
	}
	got := byHash()
	for hash, want := range map[string]ledger.Class{
		"a1": ledger.ClassCEXWithdrawal, "a2": ledger.ClassOutflow, "a3": ledger.ClassOutflow, "b1": ledger.ClassInflow,
	} {
		if got[hash].Class != want {
			t.Errorf("%s class = %s, want %s", hash, got[hash].Class, want)
		}
	}
	if got["b1"].Asset.Symbol != "USDT" || got["b1"].Amount() != "2.5" {
		t.Errorf("b1 asset = %+v amount %s", got["b1"].Asset, got["b1"].Amount())
	}

	// Inbox: a2 and b1 (a3 is zero, a1 is an exchange withdrawal).
	if n, _ := s.CountTransfers(ctx, Filter{Inbox: true}); n != 2 {
		t.Errorf("inbox = %d, want 2", n)
	}
	if err := s.AddRule(ctx, ledger.Rule{Counterparty: shop, Class: ledger.ClassOutflow, Category: "Оплата"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountTransfers(ctx, Filter{Inbox: true}); n != 1 {
		t.Errorf("inbox after rule = %d, want 1", n)
	}
	if c := byHash()["a2"].Category; c != "Оплата" {
		t.Errorf("category = %q", c)
	}

	// Marking the payer as mine turns the inflow into an internal transfer.
	if err := s.UpsertAddress(ctx, ledger.Address{Family: "evm", Address: payer, Kind: ledger.KindMine}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reclassify(ctx); err != nil {
		t.Fatal(err)
	}
	if c := byHash()["b1"].Class; c != ledger.ClassInternal {
		t.Errorf("class after marking = %s", c)
	}

	// Filters by side.
	if n, _ := s.CountTransfers(ctx, Filter{FromAddrs: []string{mine}, ToAddrs: []string{shop}}); n != 2 {
		t.Errorf("mine→shop = %d, want 2", n)
	}
}

func TestPurgeOrphans(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	const mine, cex, x = "TMine", "TCex", "TX"
	s.UpsertAddress(ctx, ledger.Address{Family: "tron", Address: mine, Kind: ledger.KindMine})
	s.UpsertAddress(ctx, ledger.Address{Family: "tron", Address: cex, Kind: ledger.KindMine}) // the mistake
	usdt := ledger.Asset{Chain: "tron", Contract: "T1", Symbol: "USDT", Decimals: 6}
	s.InsertTransfers(ctx, []ledger.Transfer{
		{UID: "1", Chain: "tron", TxHash: "a", From: cex, To: mine, Asset: usdt, AmountRaw: "1", FeeRaw: "0"},
		{UID: "2", Chain: "tron", TxHash: "b", From: x, To: cex, Asset: usdt, AmountRaw: "1", FeeRaw: "0"},
		{UID: "3", Chain: "tron", TxHash: "c", From: cex, To: x, Asset: usdt, AmountRaw: "1", FeeRaw: "0"},
		{UID: "4", Chain: "tron", TxHash: "d", From: x, To: cex, Asset: usdt, AmountRaw: "1", FeeRaw: "0"},
	})
	s.SetCursor(ctx, "tron", cex, "trc20", 123)
	all, _ := s.Transfers(ctx, Filter{})
	for _, tr := range all {
		if tr.TxHash == "d" {
			s.SetComment(ctx, tr.ID, "keep me")
		}
	}

	// Fix the mistake: the exchange is not mine.
	s.UpsertAddress(ctx, ledger.Address{Family: "tron", Address: cex, Kind: ledger.KindExchange})
	if err := s.RefreshBook(ctx); err != nil {
		t.Fatal(err)
	}
	left, _ := s.Transfers(ctx, Filter{})
	hashes := map[string]bool{}
	for _, tr := range left {
		hashes[tr.TxHash] = true
	}
	if !hashes["a"] || hashes["b"] || hashes["c"] || !hashes["d"] {
		t.Errorf("left after purge: %v", hashes)
	}
	if c := s.Cursor(ctx, "tron", cex, "trc20"); c != 0 {
		t.Errorf("cursor kept: %d", c)
	}
	if n, _ := s.CountTransfers(ctx, Filter{Inbox: true}); n != 0 {
		t.Errorf("inbox = %d, want 0 (a is a withdrawal, d is exchange↔external)", n)
	}
}
