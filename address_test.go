package main

import "testing"

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		in, family, addr string
		ok               bool
	}{
		{"0xAbC0000000000000000000000000000000000001", FamilyEVM, "0xabc0000000000000000000000000000000000001", true},
		{"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", FamilyTron, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t", true},
		{"TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6u", "", "", false},
		{"hello", "", "", false},
		{"0x123", "", "", false},
	}
	for _, tt := range tests {
		f, a, err := normalizeAddress(tt.in)
		if (err == nil) != tt.ok || f != tt.family || a != tt.addr {
			t.Errorf("normalizeAddress(%q) = %q, %q, %v", tt.in, f, a, err)
		}
	}
}

func TestTronFromHex(t *testing.T) {
	// USDT TRC20 contract.
	if got := tronFromHex("41a614f803b6fd780986a42c78ec9c7f77e6ded13c"); got != "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" {
		t.Errorf("tronFromHex = %s", got)
	}
}
