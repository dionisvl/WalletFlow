// Package ledger holds the domain model: addresses, assets, transfers and their classification.
package ledger

import "strings"

// Class describes how a transfer relates to the owner.
type Class string

const (
	ClassInternal      Class = "internal"
	ClassCEXDeposit    Class = "cex_deposit"
	ClassCEXWithdrawal Class = "cex_withdrawal"
	ClassInflow        Class = "inflow"
	ClassOutflow       Class = "outflow"
	ClassUnknown       Class = "unknown"
)

// Kind is the owner type of an address.
type Kind string

const (
	KindMine     Kind = "mine"
	KindExchange Kind = "exchange"
	KindExternal Kind = "external"
	// KindWatch is someone else's wallet we sync to study its links (graph),
	// kept out of the owner's ledger: for classification it is external.
	KindWatch Kind = "watch"
)

// Kinds lists all address kinds in display order.
var Kinds = []Kind{KindMine, KindExchange, KindExternal, KindWatch}

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	switch k {
	case KindMine, KindExchange, KindExternal, KindWatch:
		return true
	}
	return false
}

// Classify returns the class for a transfer based on both sides.
// Addresses missing from the address book are KindExternal.
func Classify(from, to Kind) Class {
	if from == KindWatch {
		from = KindExternal
	}
	if to == KindWatch {
		to = KindExternal
	}
	switch {
	case from == KindMine && to == KindMine:
		return ClassInternal
	case from == KindMine && to == KindExchange:
		return ClassCEXDeposit
	case from == KindExchange && to == KindMine:
		return ClassCEXWithdrawal
	case to == KindMine:
		return ClassInflow
	case from == KindMine:
		return ClassOutflow
	default:
		return ClassUnknown
	}
}

// NeedsReview reports whether a class goes to the Inbox.
func (c Class) NeedsReview() bool {
	return c == ClassInflow || c == ClassOutflow || c == ClassUnknown
}

// Incoming reports whether funds come to the owner.
func (c Class) Incoming() bool { return c == ClassInflow || c == ClassCEXWithdrawal }

// Classes lists all classes in display order.
var Classes = []Class{ClassInflow, ClassOutflow, ClassInternal, ClassCEXDeposit, ClassCEXWithdrawal, ClassUnknown}

var classLabels = map[Class]string{
	ClassInternal:      "Внутренний",
	ClassCEXDeposit:    "На биржу",
	ClassCEXWithdrawal: "С биржи",
	ClassInflow:        "Входящий",
	ClassOutflow:       "Исходящий",
	ClassUnknown:       "Неизвестно",
}

// Label is the human name of the class.
func (c Class) Label() string { return classLabels[c] }

var kindLabels = map[Kind]string{KindMine: "Мой", KindExchange: "Биржа", KindExternal: "Чужой", KindWatch: "Наблюдаемый"}

// Synced reports whether addresses of this kind get their history downloaded.
func (k Kind) Synced() bool { return k == KindMine || k == KindWatch }

// Label is the human name of the kind.
func (k Kind) Label() string { return kindLabels[k] }

// ParseKind accepts English and Russian names; it returns "" when unknown.
func ParseKind(s string) Kind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mine", "my", "мой", "мои", "свой":
		return KindMine
	case "exchange", "cex", "биржа", "биржевой":
		return KindExchange
	case "external", "other", "чужой", "чужие":
		return KindExternal
	case "watch", "watched", "наблюдаемый", "наблюдать", "следить":
		return KindWatch
	}
	return ""
}
