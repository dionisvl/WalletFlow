package web

import (
	"html/template"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/dionisvl/WalletFlow/internal/address"
	"github.com/dionisvl/WalletFlow/internal/chain"
	"github.com/dionisvl/WalletFlow/internal/i18n"
	"github.com/dionisvl/WalletFlow/internal/ledger"
	"github.com/dionisvl/WalletFlow/internal/syncer"
)

// Page is the data every template gets; page specific data is in Data.
type Page struct {
	Title      string
	Active     string
	InboxCount int
	Categories []string
	Chains     []chain.Chain
	Sync       syncer.Status
	Book       ledger.Book
	Data       any
	Query      url.Values
	Lang       i18n.Lang
	Langs      []i18n.Lang
	JSDict     map[string]string // translations for the browser scripts
	Demo       bool
}

func (p Page) Label(addr string) string { return p.Book.Label(addr) }

func (p Page) Known(addr string) bool {
	_, ok := p.Book[addr]
	return ok
}

func (p Page) Q(key string) string { return p.Query.Get(key) }

// EVMChains lists the names of enabled EVM chains, comma separated.
func (p Page) EVMChains() string {
	var names []string
	for _, c := range p.Chains {
		if c.Family == address.EVM {
			names = append(names, c.Name)
		}
	}
	return strings.Join(names, ", ")
}

// Fee formats the fee of t when the owner paid it.
func (p Page) Fee(t ledger.Transfer) string {
	if t.FeeRaw == "" || t.FeeRaw == "0" || !p.Book.IsMine(t.From) {
		return ""
	}
	c, _ := chain.ByKey(t.Chain)
	return ledger.FormatAmount(t.FeeRaw, c.Decimals) + " " + c.Symbol
}

// Lookalike returns the name of a known address that addr imitates:
// same first and last 4 characters but a different address (address poisoning).
func (p Page) Lookalike(addr string) string {
	if p.Known(addr) || len(addr) < 12 {
		return ""
	}
	start := 2 // skip 0x
	if !strings.HasPrefix(addr, "0x") {
		start = 1 // skip T
	}
	for _, a := range p.Book {
		k := a.Address
		if len(k) == len(addr) && k[start:start+4] == addr[start:start+4] && k[len(k)-4:] == addr[len(addr)-4:] {
			return a.Label()
		}
	}
	return ""
}

// rowData lets a row partial see both the page and its transfer.
type rowData struct {
	P Page
	T ledger.Transfer
}

// funcsFor returns template functions with text translated to l.
func funcsFor(l i18n.Lang) template.FuncMap {
	return template.FuncMap{
		"t":          l.T,
		"short":      address.Short,
		"explorer":   chain.TxURL,
		"chainName":  chain.Name,
		"kindLabel":  func(k ledger.Kind) string { return l.T(k.Label()) },
		"classLabel": func(c ledger.Class) string { return l.T(c.Label()) },
		"date":       func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"add1":       func(i int) int { return i + 1 },
		"kinds":      func() []ledger.Kind { return ledger.Kinds },
		"classes":    func() []ledger.Class { return ledger.Classes },
		"inList":     func(s string, list []string) bool { return slices.Contains(list, s) },
		"pair":       func(p Page, t ledger.Transfer) rowData { return rowData{p, t} },
	}
}
