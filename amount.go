package main

import (
	"math/big"
	"strings"
)

// formatAmount turns an integer string in minimal units into a decimal string.
func formatAmount(raw string, decimals int) string {
	n, ok := new(big.Int).SetString(raw, 10)
	if !ok {
		return raw
	}
	neg := n.Sign() < 0
	s := new(big.Int).Abs(n).String()
	if decimals > 0 {
		if len(s) <= decimals {
			s = strings.Repeat("0", decimals-len(s)+1) + s
		}
		s = s[:len(s)-decimals] + "." + s[len(s)-decimals:]
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if neg {
		s = "-" + s
	}
	return s
}

// amountFloat is for charts only, never for accounting.
func amountFloat(raw string, decimals int) float64 {
	f, _, err := big.ParseFloat(formatAmount(raw, decimals), 10, 64, big.ToNearestEven)
	if err != nil {
		return 0
	}
	v, _ := f.Float64()
	return v
}

// mulRaw multiplies two integer strings, used for gas fees.
func mulRaw(a, b string) string {
	x, ok1 := new(big.Int).SetString(a, 10)
	y, ok2 := new(big.Int).SetString(b, 10)
	if !ok1 || !ok2 {
		return "0"
	}
	return x.Mul(x, y).String()
}
