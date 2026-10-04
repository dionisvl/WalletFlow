// Package demo fills a database with made-up wallets and transfers, so the app
// can be tried and screenshotted without API keys or real addresses.
package demo

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/dionisvl/walletflow/internal/address"
	"github.com/dionisvl/walletflow/internal/ledger"
	"github.com/dionisvl/walletflow/internal/store"
)

// Categories are the demo's categories; they match the transfers it reviews.
var Categories = []string{"Purchase", "P2P sale", "Income", "Payment", "Gift", "Swap", "Refund", "Other"}

var (
	eth     = ledger.Asset{Chain: "ethereum", Symbol: "ETH", Decimals: 18}
	usdtEth = ledger.Asset{Chain: "ethereum", Contract: "0xdac17f958d2ee523a2206206994597c13d831ec7", Symbol: "USDT", Decimals: 6}
	usdc    = ledger.Asset{Chain: "ethereum", Contract: "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48", Symbol: "USDC", Decimals: 6}
	spam    = ledger.Asset{Chain: "ethereum", Contract: "0x00000000000000000000000000000000000dead1", Symbol: "Visit claim-usdt.xyz", Decimals: 18}
	trx     = ledger.Asset{Chain: "tron", Symbol: "TRX", Decimals: 6}
	usdtTrx = ledger.Asset{Chain: "tron", Contract: "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", Symbol: "USDT", Decimals: 6}
)

type seeder struct {
	rnd *rand.Rand
	now time.Time
	ts  []ledger.Transfer
	n   int
}

// Seed writes the demo data set. now anchors the timeline so charts look current.
func Seed(ctx context.Context, st *store.Store, now time.Time) error {
	s := &seeder{rnd: rand.New(rand.NewPCG(42, 7)), now: now}

	ledgerW, savings := s.evm(), s.evm()
	hot := s.tron()
	binanceEVM, binanceTron, kraken := s.evm(), s.tron(), s.evm()
	landlord, alice, employer := s.tron(), s.tron(), s.tron()
	suspectA, suspectB, hub := s.tron(), s.tron(), s.tron()
	book := []ledger.Address{
		{Family: address.EVM, Address: ledgerW, Name: "Ledger", Kind: ledger.KindMine, Color: "#2f6fed"},
		{Family: address.EVM, Address: savings, Name: "Savings", Kind: ledger.KindMine, Color: "#0ea5e9"},
		{Family: address.Tron, Address: hot, Name: "Hot wallet", Kind: ledger.KindMine, Color: "#6366f1"},
		{Family: address.EVM, Address: binanceEVM, Name: "Binance", Kind: ledger.KindExchange},
		{Family: address.Tron, Address: binanceTron, Name: "Binance", Kind: ledger.KindExchange},
		{Family: address.EVM, Address: kraken, Name: "Kraken", Kind: ledger.KindExchange},
		{Family: address.Tron, Address: landlord, Name: "Landlord", Kind: ledger.KindExternal},
		{Family: address.Tron, Address: alice, Name: "Alice (P2P)", Kind: ledger.KindExternal},
		{Family: address.Tron, Address: employer, Name: "Employer", Kind: ledger.KindExternal},
		{Family: address.Tron, Address: suspectA, Name: "Suspect A", Kind: ledger.KindWatch},
		{Family: address.Tron, Address: suspectB, Name: "Suspect B", Kind: ledger.KindWatch},
	}
	for _, a := range book {
		if err := st.UpsertAddress(ctx, a); err != nil {
			return err
		}
	}

	const months = 14
	start := now.AddDate(0, -months, 0)
	for m := range months {
		month := start.AddDate(0, m, 0)
		recent := m >= months-2 // the last two months stay unreviewed in the Inbox
		// Salary in USDT on Tron, categorized by a rule.
		s.add(employer, hot, usdtTrx, s.amount(3000, 3600, 6), month.AddDate(0, 0, 1), "", "")
		// Rent.
		s.add(hot, landlord, usdtTrx, s.amount(1200, 1200, 6), month.AddDate(0, 0, 3), s.catUnless(recent, "Payment"), "")
		// Savings go to the exchange.
		s.add(hot, binanceTron, usdtTrx, s.amount(800, 1500, 6), month.AddDate(0, 0, 10), "", "")
		// TRX for fees, every few months.
		if m%3 == 0 {
			s.add(binanceTron, hot, trx, s.amount(150, 250, 6), month.AddDate(0, 0, 9), "", "")
		}
		// ETH bought on the exchange lands on the Ledger.
		if m%2 == 0 {
			s.add(binanceEVM, ledgerW, eth, s.amount(0.3, 1.2, 18), month.AddDate(0, 0, 12), "", "")
		}
		// Long-term savings.
		if m%4 == 1 {
			s.add(ledgerW, savings, eth, s.amount(0.2, 0.5, 18), month.AddDate(0, 0, 15), "", "")
		}
		// P2P: selling USDT to Alice and buying back.
		if m%2 == 1 {
			s.add(hot, alice, usdtTrx, s.amount(300, 700, 6), month.AddDate(0, 0, 18), s.catUnless(recent, "P2P sale"), "bank transfer received")
		}
		// Spending from the Ledger at random shops.
		for i := range s.rnd.IntN(3) {
			s.add(ledgerW, s.evm(), usdc, s.amount(20, 400, 6), month.AddDate(0, 0, 20+i), s.catUnless(recent, "Purchase"), "")
		}
		// Small incoming transfers from strangers.
		if s.rnd.IntN(2) == 0 {
			s.add(s.tron(), hot, usdtTrx, s.amount(5, 80, 6), month.AddDate(0, 0, 22), s.catUnless(recent, "Gift"), "")
		}
	}
	// Kraken: an old account that was closed.
	s.add(kraken, ledgerW, usdtEth, s.amount(2500, 2500, 6), start.AddDate(0, 0, 5), "", "")
	s.add(ledgerW, kraken, usdtEth, s.amount(400, 400, 6), start.AddDate(0, 1, 5), "", "")

	// Address poisoning: a lookalike of Savings sends dust to the Ledger.
	fake := lookalike(savings, s.rnd)
	s.add(fake, ledgerW, usdtEth, "1", now.AddDate(0, 0, -6), "", "")
	// Spam airdrops.
	s.add(s.evm(), ledgerW, spam, s.amount(1000, 5000, 18), now.AddDate(0, 0, -4), "", "")
	s.add(s.evm(), savings, spam, s.amount(1000, 5000, 18), now.AddDate(0, 0, -3), "", "")

	// Two watched wallets with no direct transfers, linked through a hub,
	// and one of them once paid the hot wallet.
	for i := range 6 {
		day := now.AddDate(0, 0, -40+i*6)
		s.add(suspectA, hub, usdtTrx, s.amount(500, 3000, 6), day, "", "")
		s.add(hub, suspectB, usdtTrx, s.amount(400, 2800, 6), day.Add(3*time.Hour), "", "")
		s.add(s.tron(), suspectA, usdtTrx, s.amount(100, 2000, 6), day.Add(-5*time.Hour), "", "")
	}
	s.add(suspectB, hot, usdtTrx, s.amount(150, 150, 6), now.AddDate(0, 0, -2), "", "")

	if _, err := st.InsertTransfers(ctx, s.ts); err != nil {
		return err
	}
	if err := st.SetSetting(ctx, "categories", strings.Join(Categories, "\n")); err != nil {
		return err
	}
	if err := st.RefreshBook(ctx); err != nil {
		return err
	}
	if err := st.AddRule(ctx, ledger.Rule{Counterparty: employer, Class: ledger.ClassInflow, Category: "Income"}); err != nil {
		return err
	}
	return s.review(ctx, st)
}

// review applies the categories and comments chosen while generating.
func (s *seeder) review(ctx context.Context, st *store.Store) error {
	all, err := st.Transfers(ctx, store.Filter{})
	if err != nil {
		return err
	}
	byUID := map[string]ledger.Transfer{}
	for _, t := range all {
		byUID[t.UID] = t
	}
	for _, t := range s.ts {
		saved, ok := byUID[t.UID]
		if !ok {
			continue
		}
		if t.Category != "" && saved.Category == "" {
			if err := st.SetCategory(ctx, []int64{saved.ID}, t.Category); err != nil {
				return err
			}
		}
		if t.Comment != "" {
			if err := st.SetComment(ctx, saved.ID, t.Comment); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *seeder) add(from, to string, a ledger.Asset, amount string, at time.Time, category, comment string) {
	s.n++
	at = at.Add(time.Duration(s.rnd.IntN(12*3600)) * time.Second)
	if at.After(s.now) {
		at = s.now.Add(-time.Hour)
	}
	t := ledger.Transfer{
		Chain: a.Chain, TS: at.Unix(), From: from, To: to, Asset: a, AmountRaw: amount,
		FeeRaw: "0", Category: category, Comment: comment,
	}
	if a.Chain == "tron" {
		t.TxHash = s.hex(32)
		t.FeeRaw = fmt.Sprint(1_000_000 + s.rnd.IntN(13_000_000)) // 1–14 TRX
	} else {
		t.TxHash = "0x" + s.hex(32)
		t.FeeRaw = fmt.Sprint(int64(400_000+s.rnd.IntN(1_600_000)) * 1_000_000_000) // 0.0004–0.002 ETH
	}
	t.UID = fmt.Sprintf("%s:%s:demo:%d", t.Chain, t.TxHash, s.n)
	s.ts = append(s.ts, t)
}

func (s *seeder) catUnless(skip bool, category string) string {
	if skip {
		return ""
	}
	return category
}

// amount returns a random value in [lo, hi] as an integer string in minimal units.
func (s *seeder) amount(lo, hi float64, decimals int) string {
	v := lo + s.rnd.Float64()*(hi-lo)
	cents := int64(v * 100)
	raw := fmt.Sprint(cents) + strings.Repeat("0", max(decimals-2, 0))
	return strings.TrimLeft(raw, "0")
}

func (s *seeder) hex(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(s.rnd.UintN(256))
	}
	return hex.EncodeToString(b)
}

func (s *seeder) evm() string { return "0x" + s.hex(20) }

func (s *seeder) tron() string { return address.TronFromHex("41" + s.hex(20)) }

// lookalike keeps the first and last four characters of an EVM address.
func lookalike(addr string, rnd *rand.Rand) string {
	b := []byte(addr)
	for i := 6; i < len(b)-4; i++ {
		b[i] = "0123456789abcdef"[rnd.IntN(16)]
	}
	return string(b)
}
