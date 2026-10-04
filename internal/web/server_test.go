package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dionisvl/WalletFlow/internal/ledger"
)

func TestLocalOnly(t *testing.T) {
	h := localOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	tests := []struct {
		method, host, origin string
		want                 int
	}{
		{"GET", "127.0.0.1:8080", "", 200},
		{"POST", "localhost:8080", "http://localhost:8080", 200},
		{"POST", "127.0.0.1:8080", "https://evil.example", 403},
		{"GET", "evil.example", "", 403},
	}
	for _, tt := range tests {
		r := httptest.NewRequest(tt.method, "/", nil)
		r.Host = tt.host
		if tt.origin != "" {
			r.Header.Set("Origin", tt.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tt.want {
			t.Errorf("%s %s origin %q = %d, want %d", tt.method, tt.host, tt.origin, w.Code, tt.want)
		}
	}
}

func TestLookalike(t *testing.T) {
	mine := "0x1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa5678"
	p := Page{Book: ledger.Book{mine: {Address: mine, Name: "Ledger", Kind: ledger.KindMine}}}
	if got := p.Lookalike("0x1234bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb5678"); got != "Ledger" {
		t.Errorf("poison not detected: %q", got)
	}
	if got := p.Lookalike(mine); got != "" {
		t.Errorf("own address flagged: %q", got)
	}
	if got := p.Lookalike("0x9999bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb5678"); got != "" {
		t.Errorf("false positive: %q", got)
	}
}
