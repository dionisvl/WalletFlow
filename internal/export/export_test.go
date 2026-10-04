package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"walletflow/internal/ledger"
)

func sample() ([]ledger.Transfer, []ledger.Address) {
	addrs := []ledger.Address{{Address: "me", Name: "Main", Kind: ledger.KindMine, Family: "tron"}}
	ts := []ledger.Transfer{{Chain: "tron", TxHash: "h1", TS: 1700000000, From: "me", To: "x",
		Asset: ledger.Asset{Chain: "tron", Symbol: "TRX", Decimals: 6}, AmountRaw: "1500000", FeeRaw: "1000000",
		Class: ledger.ClassOutflow, Category: "Оплата"}}
	return ts, addrs
}

func TestCSV(t *testing.T) {
	ts, addrs := sample()
	var b bytes.Buffer
	if err := CSV(&b, ts, addrs); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.HasPrefix(out, "\xEF\xBB\xBF") || !strings.Contains(out, "Main") || !strings.Contains(out, ",1.5,1500000,1,TRX,") {
		t.Errorf("csv = %q", out)
	}
}

func TestXLSX(t *testing.T) {
	ts, addrs := sample()
	var b bytes.Buffer
	if err := XLSX(&b, ts, addrs); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&b)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := f.GetCellValue("Журнал", "J2")
	if v != "1.5" {
		t.Errorf("amount cell = %q", v)
	}
	if name, _ := f.GetCellValue("Адреса", "C2"); name != "Main" {
		t.Errorf("address sheet = %q", name)
	}
}
