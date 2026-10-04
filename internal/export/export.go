// Package export writes the journal as .xlsx or .csv.
package export

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/xuri/excelize/v2"

	"walletflow/internal/chain"
	"walletflow/internal/ledger"
)

var header = []string{
	"Дата (UTC)", "Сеть", "Класс", "Категория", "Откуда", "Откуда (имя)", "Куда", "Куда (имя)",
	"Актив", "Сумма", "Сумма (raw)", "Комиссия", "Актив комиссии", "TxHash", "Ссылка", "Комментарий",
}

func rows(ts []ledger.Transfer, addrs []ledger.Address) [][]any {
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
			t.Time().Format("2006-01-02 15:04:05"), chain.Name(t.Chain), t.Class.Label(), t.Category,
			t.From, label(t.From), t.To, label(t.To),
			t.Asset.Symbol, t.Amount(), t.AmountRaw, fee, feeAsset, t.TxHash, chain.TxURL(t.Chain, t.TxHash), t.Comment,
		})
	}
	return rows
}

// XLSX writes the journal and the address book as two sheets.
func XLSX(w io.Writer, ts []ledger.Transfer, addrs []ledger.Address) error {
	f := excelize.NewFile()
	defer f.Close()
	const journal = "Журнал"
	f.SetSheetName("Sheet1", journal)
	sw, err := f.NewStreamWriter(journal)
	if err != nil {
		return err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	head := make([]any, len(header))
	for i, h := range header {
		head[i] = excelize.Cell{StyleID: bold, Value: h}
	}
	sw.SetColWidth(1, 1, 18)
	sw.SetColWidth(5, 8, 22)
	sw.SetColWidth(10, 10, 16)
	if err := sw.SetRow("A1", head, excelize.RowOpts{}); err != nil {
		return err
	}
	for i, row := range rows(ts, addrs) {
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

	const book = "Адреса"
	f.NewSheet(book)
	f.SetSheetRow(book, "A1", &[]any{"Адрес", "Сеть", "Имя", "Тип"})
	f.SetRowStyle(book, 1, 1, bold)
	f.SetColWidth(book, "A", "A", 46)
	for i, a := range addrs {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		f.SetSheetRow(book, cell, &[]any{a.Address, a.Family, a.Name, a.Kind.Label()})
	}
	_, err = f.WriteTo(w)
	return err
}

// CSV writes the journal with a UTF-8 BOM so Excel opens it correctly.
func CSV(w io.Writer, ts []ledger.Transfer, addrs []ledger.Address) error {
	io.WriteString(w, "\xEF\xBB\xBF") // BOM so Excel reads UTF-8
	cw := csv.NewWriter(w)
	cw.Write(header)
	for _, row := range rows(ts, addrs) {
		rec := make([]string, len(row))
		for i, v := range row {
			rec[i], _ = v.(string)
		}
		cw.Write(rec)
	}
	cw.Flush()
	return cw.Error()
}
