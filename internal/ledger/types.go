package ledger

import (
	"time"

	"walletflow/internal/address"
)

// Address is an entry of the address book.
// Addresses sharing a Name form one entity (e.g. all Binance addresses).
type Address struct {
	ID      int64
	Family  string // address.EVM | address.Tron
	Address string
	Name    string
	Kind    Kind
	Color   string
}

// Label is the name, or the short address when unnamed.
func (a Address) Label() string {
	if a.Name != "" {
		return a.Name
	}
	return address.Short(a.Address)
}

// Book maps an address to its address book entry.
type Book map[string]Address

// KindOf returns the kind of addr; unknown addresses are external.
func (b Book) KindOf(addr string) Kind {
	if a, ok := b[addr]; ok {
		return a.Kind
	}
	return KindExternal
}

// IsMine reports whether addr belongs to the owner.
func (b Book) IsMine(addr string) bool { return b[addr].Kind == KindMine }

// Label returns the book name of addr, or its short form.
func (b Book) Label(addr string) string {
	if a, ok := b[addr]; ok {
		return a.Label()
	}
	return address.Short(addr)
}

// Asset is a coin or token on one chain.
type Asset struct {
	ID       int64
	Chain    string
	Contract string // "" = native coin
	Symbol   string
	Decimals int
	IsSpam   bool
}

// Transfer is one movement of one asset. A transaction can hold several.
type Transfer struct {
	ID        int64
	UID       string // chain:hash:stream:index, unique
	Chain     string
	TxHash    string
	TS        int64
	From      string
	To        string
	Asset     Asset
	AmountRaw string // integer in minimal units
	FeeRaw    string // native coin, minimal units
	Class     Class
	Category  string // "" = not reviewed
	Comment   string
}

func (t Transfer) Time() time.Time { return time.Unix(t.TS, 0).UTC() }
func (t Transfer) Amount() string  { return FormatAmount(t.AmountRaw, t.Asset.Decimals) }
func (t Transfer) Incoming() bool  { return t.Class.Incoming() }

// Counterparty is the side that is not the owner.
func (t Transfer) Counterparty() string {
	if t.Class.Incoming() {
		return t.From
	}
	return t.To
}

// Rule assigns a category to every inflow from, or outflow to, an address.
type Rule struct {
	ID           int64
	Counterparty string
	Class        Class
	Category     string
}
