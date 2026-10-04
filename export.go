package main

import (
	"encoding/csv"
	"io"
	"strconv"

	"github.com/xuri/excelize/v2"
)

var exportHeader = []string{
	"Дата (UTC)", "Сеть", "Класс", "Категория", "Откуда", "Откуда (имя)", "Куда", "Куда (имя)",
	"Актив", "Сумма", "Сумма (raw)", "Комиссия", "Актив комиссии", "TxHash", "Ссылка", "Комментарий",
}

func exportRows(ts []Transfer, addrs []Address) [][]any {
	names := map[string]Address{}
	for _, a := range addrs {
		names[a.Address] = a
	}
	p := Page{Book: names}
	rows := make([][]any, 0, len(ts))
	for _, t := range ts {
		fee, feeAsset := "", ""
		if t.FeeRaw != "0" && p.KindOf(t.From) == KindMine {
			c, _ := chainByKey(t.Chain)
			fee, feeAsset = formatAmount(t.FeeRaw, c.Decimals), c.Symbol
		}
		label := func(a string) string {
			if ad, ok := names[a]; ok {
				return ad.Name
			}
			return ""
		}
		rows = append(rows, []any{
			t.Time().Format("2006-01-02 15:04:05"), chainName(t.Chain), classLabels[t.Class], t.Category,
			t.From, label(t.From), t.To, label(t.To),
			t.Symbol, t.Amount(), t.AmountRaw, fee, feeAsset, t.TxHash, explorerURL(t.Chain, t.TxHash), t.Comment,
		})
	}
	return rows
}

func writeXLSX(w io.Writer, ts []Transfer, addrs []Address) error {
	f := excelize.NewFile()
	defer f.Close()
	const journal = "Журнал"
	f.SetSheetName("Sheet1", journal)
	sw, err := f.NewStreamWriter(journal)
	if err != nil {
		return err
	}
	bold, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	head := make([]any, len(exportHeader))
	for i, h := range exportHeader {
		head[i] = excelize.Cell{StyleID: bold, Value: h}
	}
	sw.SetColWidth(1, 1, 18)
	sw.SetColWidth(5, 8, 22)
	sw.SetColWidth(10, 10, 16)
	if err := sw.SetRow("A1", head, excelize.RowOpts{}); err != nil {
		return err
	}
	for i, row := range exportRows(ts, addrs) {
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
		f.SetSheetRow(book, cell, &[]any{a.Address, a.Family, a.Name, kindLabels[a.Kind]})
	}
	_, err = f.WriteTo(w)
	return err
}

func writeCSV(w io.Writer, ts []Transfer, addrs []Address) error {
	io.WriteString(w, "\xEF\xBB\xBF") // BOM so Excel reads UTF-8
	cw := csv.NewWriter(w)
	cw.Write(exportHeader)
	for _, row := range exportRows(ts, addrs) {
		rec := make([]string, len(row))
		for i, v := range row {
			rec[i], _ = v.(string)
		}
		cw.Write(rec)
	}
	cw.Flush()
	return cw.Error()
}
