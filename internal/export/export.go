// Package export writes the journal as .xlsx or .csv.
package export

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/xuri/excelize/v2"

	"github.com/dionisvl/walletflow/internal/chain"
	"github.com/dionisvl/walletflow/internal/ledger"
)

// T translates UI text; nil means English.
type T func(s string, args ...any) string

func (t T) tr(s string) string {
	if t == nil {
		return s
	}
	return t(s)
}

var header = []string{
	"Date (UTC)", "Network", "Class", "Category", "From", "From (name)", "To", "To (name)",
	"Asset", "Amount", "Amount (raw)", "Fee", "Fee asset", "TxHash", "Link", "Comment",
}

func (t T) header() []string {
	out := make([]string, len(header))
	for i, h := range header {
		out[i] = t.tr(h)
	}
	return out
}

func rows(ts []ledger.Transfer, addrs []ledger.Address, tr T) [][]any {
	names := ledger.Book{}
	for _, a := range addrs {
		names[a.Address] = a
	}
	rows := make([][]any, 0, len(ts))
	for _, t := range ts {
		fee, feeAsset := "", ""
		if t.FeeRaw != "0" && names.IsMine(t.From) {
			c, _ := chain.ByKey(t.Chain)
			fee, feeAsset = ledger.FormatAmount(t.FeeRaw, c.Decimals), c.Symbol
		}
		label := func(a string) string {
			if ad, ok := names[a]; ok {
				return ad.Name
			}
			return ""
		}
		rows = append(rows, []any{
			t.Time().Format("2006-01-02 15:04:05"), chain.Name(t.Chain), tr.tr(t.Class.Label()), t.Category,
			t.From, label(t.From), t.To, label(t.To),
			t.Asset.Symbol, t.Amount(), t.AmountRaw, fee, feeAsset, t.TxHash, chain.TxURL(t.Chain, t.TxHash), t.Comment,
		})
	}
	return rows
}

// XLSX writes the journal and the address book as two sheets.
func XLSX(w io.Writer, ts []ledger.Transfer, addrs []ledger.Address, tr T) error {
	f := excelize.NewFile()
	defer f.Close()
	journal := tr.tr("Journal")
	f.SetSheetName("Sheet1", journal)
	sw, err := f.NewStreamWriter(journal)
	if err != nil {
		return err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	head := make([]any, len(header))
	for i, h := range tr.header() {
		head[i] = excelize.Cell{StyleID: bold, Value: h}
	}
	sw.SetColWidth(1, 1, 18)
	sw.SetColWidth(5, 8, 22)
	sw.SetColWidth(10, 10, 16)
	if err := sw.SetRow("A1", head, excelize.RowOpts{}); err != nil {
		return err
	}
	for i, row := range rows(ts, addrs, tr) {
		// Numbers in Excel so sums work; the exact value stays in the raw column.
		for _, c := range []int{9, 11} {
			if f, err := strconv.ParseFloat(row[c].(string), 64); err == nil {
				row[c] = f
			}
		}
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := sw.SetRow(cell, row); err != nil {
			return err
		}
	}
	if err := sw.Flush(); err != nil {
		return err
	}
	f.SetPanes(journal, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	book := tr.tr("Addresses")
	f.NewSheet(book)
	f.SetSheetRow(book, "A1", &[]any{tr.tr("Address"), tr.tr("Network"), tr.tr("Name"), tr.tr("Kind")})
	f.SetRowStyle(book, 1, 1, bold)
	f.SetColWidth(book, "A", "A", 46)
	for i, a := range addrs {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		f.SetSheetRow(book, cell, &[]any{a.Address, a.Family, a.Name, tr.tr(a.Kind.Label())})
	}
	_, err = f.WriteTo(w)
	return err
}

// CSV writes the journal with a UTF-8 BOM so Excel opens it correctly.
func CSV(w io.Writer, ts []ledger.Transfer, addrs []ledger.Address, tr T) error {
	io.WriteString(w, "\xEF\xBB\xBF") // BOM so Excel reads UTF-8
	cw := csv.NewWriter(w)
	cw.Write(tr.header())
	for _, row := range rows(ts, addrs, tr) {
		rec := make([]string, len(row))
		for i, v := range row {
			rec[i], _ = v.(string)
		}
		cw.Write(rec)
	}
	cw.Flush()
	return cw.Error()
}
