package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
