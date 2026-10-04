package main

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
)

var kinds = []Kind{KindMine, KindExchange, KindExternal}

func validKind(k string) bool {
	switch Kind(k) {
	case KindMine, KindExchange, KindExternal:
		return true
	}
	return false
}

// Classify returns the class for a transfer based on both sides.
// Addresses missing from the address book are KindExternal.
func Classify(from, to Kind) Class {
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

// needsReview reports whether a class goes to the Inbox.
func needsReview(c Class) bool {
	return c == ClassInflow || c == ClassOutflow || c == ClassUnknown
}

// counterparty returns the side of the transfer that is not the owner.
func counterparty(c Class, from, to string) string {
	if c == ClassInflow || c == ClassCEXWithdrawal {
		return from
	}
	return to
}
