package main

import "testing"

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		raw      string
		decimals int
		want     string
	}{
		{"0", 18, "0"},
		{"1000000000000000000", 18, "1"},
		{"1500000", 6, "1.5"},
		{"1", 6, "0.000001"},
		{"123", 0, "123"},
		{"-2500000", 6, "-2.5"},
		{"bad", 6, "bad"},
	}
	for _, tt := range tests {
		if got := formatAmount(tt.raw, tt.decimals); got != tt.want {
			t.Errorf("formatAmount(%q, %d) = %q, want %q", tt.raw, tt.decimals, got, tt.want)
		}
	}
}
