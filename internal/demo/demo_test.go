package demo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/dionisvl/WalletFlow/internal/ledger"
	"github.com/dionisvl/WalletFlow/internal/store"
)

func TestSeed(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := Seed(ctx, st, time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	inbox, _ := st.CountTransfers(ctx, store.Filter{Inbox: true})
	if inbox < 5 || inbox > 60 {
		t.Errorf("inbox = %d, want a handful to review", inbox)
	}
	for _, c := range []ledger.Class{ledger.ClassInternal, ledger.ClassCEXDeposit, ledger.ClassCEXWithdrawal, ledger.ClassInflow, ledger.ClassOutflow, ledger.ClassUnknown} {
		if n, _ := st.CountTransfers(ctx, store.Filter{Class: c}); n == 0 {
			t.Errorf("no %s transfers", c)
		}
	}
	if n, _ := st.CountTransfers(ctx, store.Filter{Category: "Income"}); n < 10 {
		t.Errorf("salary not categorized by the rule: %d", n)
	}
	all, _ := st.Transfers(ctx, store.Filter{})
	for _, tr := range all {
		if tr.AmountRaw == "" || tr.AmountRaw[0] == '-' {
			t.Fatalf("bad amount %q", tr.AmountRaw)
		}
	}
}
