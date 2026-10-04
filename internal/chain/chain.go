// Package chain lists supported networks and shared helpers for provider clients.
package chain

import (
	"slices"

	"github.com/dionisvl/walletflow/internal/address"
)

// Chain is a network we can sync.
type Chain struct {
	Key      string // stored in transfers.chain
	Name     string
	Family   string // address.EVM | address.Tron
	ChainID  int    // Etherscan V2 chainid
	Symbol   string
	Decimals int
	Explorer string // tx URL prefix
}

var all = []Chain{
	{"ethereum", "Ethereum", address.EVM, 1, "ETH", 18, "https://etherscan.io/tx/"},
	{"arbitrum", "Arbitrum", address.EVM, 42161, "ETH", 18, "https://arbiscan.io/tx/"},
	{"base", "Base", address.EVM, 8453, "ETH", 18, "https://basescan.org/tx/"},
	{"optimism", "Optimism", address.EVM, 10, "ETH", 18, "https://optimistic.etherscan.io/tx/"},
	{"polygon", "Polygon", address.EVM, 137, "POL", 18, "https://polygonscan.com/tx/"},
	{"bsc", "BNB Chain", address.EVM, 56, "BNB", 18, "https://bscscan.com/tx/"},
	{"linea", "Linea", address.EVM, 59144, "ETH", 18, "https://lineascan.build/tx/"},
	{"tron", "Tron", address.Tron, 0, "TRX", 6, "https://tronscan.org/#/transaction/"},
}

// All returns every supported chain except the ignored keys.
func All(ignore ...string) []Chain {
	var out []Chain
	for _, c := range all {
		if !slices.Contains(ignore, c.Key) {
			out = append(out, c)
		}
	}
	return out
}

// ByKey finds a chain by its key.
func ByKey(key string) (Chain, bool) {
	for _, c := range all {
		if c.Key == key {
			return c, true
		}
	}
	return Chain{}, false
}

// TxURL links a transaction in the chain explorer.
func TxURL(chain, hash string) string {
	if c, ok := ByKey(chain); ok {
		return c.Explorer + hash
	}
	return ""
}

// Name is the human name of a chain key.
func Name(key string) string {
	if c, ok := ByKey(key); ok {
		return c.Name
	}
	return key
}
