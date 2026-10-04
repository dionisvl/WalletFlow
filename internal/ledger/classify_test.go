package ledger

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		from, to Kind
		want     Class
	}{
		{KindMine, KindMine, ClassInternal},
		{KindMine, KindExchange, ClassCEXDeposit},
		{KindExchange, KindMine, ClassCEXWithdrawal},
		{KindExternal, KindMine, ClassInflow},
		{KindMine, KindExternal, ClassOutflow},
		{KindExchange, KindExchange, ClassUnknown},
		{KindExternal, KindExchange, ClassUnknown},
		{KindExternal, KindExternal, ClassUnknown},
		{KindWatch, KindMine, ClassInflow},
		{KindMine, KindWatch, ClassOutflow},
		{KindWatch, KindExchange, ClassUnknown},
	}
	for _, tt := range tests {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			if got := Classify(tt.from, tt.to); got != tt.want {
				t.Errorf("Classify(%s, %s) = %s, want %s", tt.from, tt.to, got, tt.want)
			}
		})
	}
}
