package main

// Chain is a network we can sync.
type Chain struct {
	Key      string // stored in transfers.chain
	Name     string
	Family   string
	ChainID  int // Etherscan V2 chainid
	Symbol   string
	Decimals int
	Explorer string // tx URL prefix
}

var chains = []Chain{
	{"ethereum", "Ethereum", FamilyEVM, 1, "ETH", 18, "https://etherscan.io/tx/"},
	{"arbitrum", "Arbitrum", FamilyEVM, 42161, "ETH", 18, "https://arbiscan.io/tx/"},
	{"base", "Base", FamilyEVM, 8453, "ETH", 18, "https://basescan.org/tx/"},
	{"optimism", "Optimism", FamilyEVM, 10, "ETH", 18, "https://optimistic.etherscan.io/tx/"},
	{"polygon", "Polygon", FamilyEVM, 137, "POL", 18, "https://polygonscan.com/tx/"},
	{"bsc", "BNB Chain", FamilyEVM, 56, "BNB", 18, "https://bscscan.com/tx/"},
	{"linea", "Linea", FamilyEVM, 59144, "ETH", 18, "https://lineascan.build/tx/"},
	{"tron", "Tron", FamilyTron, 0, "TRX", 6, "https://tronscan.org/#/transaction/"},
}

func chainByKey(key string) (Chain, bool) {
	for _, c := range chains {
		if c.Key == key {
			return c, true
		}
	}
	return Chain{}, false
}

func explorerURL(chain, hash string) string {
	if c, ok := chainByKey(chain); ok {
		return c.Explorer + hash
	}
	return ""
}

func chainName(key string) string {
	if c, ok := chainByKey(key); ok {
		return c.Name
	}
	return key
}

func evmChains() []Chain {
	var out []Chain
	for _, c := range chains {
		if c.Family == FamilyEVM {
			out = append(out, c)
		}
	}
	return out
}
