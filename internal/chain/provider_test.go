package chain

import "testing"

func TestPerDay(t *testing.T) {
	day := int64(86400)
	tests := []struct {
		name string
		ts   []int64
		full bool
		want float64
	}{
		{"short history", []int64{0, day}, false, 0},
		{"200 over 10 days", spread(200, 10*day), true, 20},
		{"200 in a minute counts as an hour", spread(200, 60), true, 200 * 24},
	}
	for _, tt := range tests {
		if got := PerDay(tt.ts, tt.full); got < tt.want*0.99 || got > tt.want*1.01 {
			t.Errorf("%s: PerDay = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func spread(n int, span int64) []int64 {
	out := make([]int64, n)
	for i := range out {
		out[i] = int64(i) * span / int64(n-1)
	}
	return out
}
