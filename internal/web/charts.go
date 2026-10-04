package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"walletflow/internal/ledger"
	"walletflow/internal/report"
	"walletflow/internal/store"
)

type assetsData struct {
	Assets []ledger.Asset
}

// visibleAssets lists non-spam assets for chart pickers.
func (s *Server) visibleAssets(ctx context.Context) ([]ledger.Asset, error) {
	all, err := s.store.Assets(ctx)
	if err != nil {
		return nil, err
	}
	var out []ledger.Asset
	for _, a := range all {
		if !a.IsSpam {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *Server) chartPage(name, title string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		assets, err := s.visibleAssets(r.Context())
		if err != nil {
			serverError(w, err)
			return
		}
		s.render(w, r, name, title, assetsData{Assets: assets})
	}
}

func (s *Server) flowsPage(w http.ResponseWriter, r *http.Request) {
	s.chartPage("flows", "Потоки")(w, r)
}

func (s *Server) balancesPage(w http.ResponseWriter, r *http.Request) {
	s.chartPage("balances", "Балансы")(w, r)
}

func (s *Server) graphPage(w http.ResponseWriter, r *http.Request) {
	s.chartPage("graph", "Граф связей")(w, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) flowsData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, err := s.filterFromQuery(ctx, r.URL.Query())
	if err != nil {
		serverError(w, err)
		return
	}
	if f.AssetID == 0 {
		http.Error(w, "asset required", http.StatusBadRequest)
		return
	}
	ts, err := s.store.Transfers(ctx, f)
	if err != nil {
		serverError(w, err)
		return
	}
	book, err := s.store.Book(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	minPct, _ := strconv.ParseFloat(r.URL.Query().Get("min"), 64)
	writeJSON(w, report.BuildSankey(ts, book, minPct))
}

func (s *Server) balancesData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(r.URL.Query().Get("asset"), 10, 64)
	if id == 0 {
		http.Error(w, "asset required", http.StatusBadRequest)
		return
	}
	ts, err := s.store.Transfers(ctx, store.Filter{AssetID: id})
	if err != nil {
		serverError(w, err)
		return
	}
	book, err := s.store.Book(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, report.BuildBalances(ts, book))
}

// graphData links book entries; outside addresses come in by the options:
// top senders/receivers within the last N days, and shared counterparties.
func (s *Server) graphData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	book, err := s.store.Book(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	f, err := s.filterFromQuery(ctx, q)
	if err != nil {
		serverError(w, err)
		return
	}
	f.OnlyLedger = false // watched wallets are the point here
	var ts []ledger.Transfer
	if len(book) > 0 {
		f.Addresses = make([]string, 0, len(book))
		for a := range book {
			f.Addresses = append(f.Addresses, a)
		}
		if ts, err = s.store.Transfers(ctx, f); err != nil {
			serverError(w, err)
			return
		}
	}
	days, _ := strconv.Atoi(q.Get("days"))
	days = min(max(days, 1), 3660)
	end := time.Now()
	if f.To > 0 {
		end = time.Unix(f.To, 0)
	}
	maxUnknown, err := strconv.Atoi(q.Get("max"))
	if err != nil {
		maxUnknown = 25
	}
	writeJSON(w, report.BuildGraph(ts, book, report.GraphOptions{
		Group:        q.Get("group") != "0",
		UnknownIn:    q.Get("unknown_in") != "",
		UnknownOut:   q.Get("unknown_out") != "",
		UnknownSince: end.AddDate(0, 0, -days).Unix(),
		MaxUnknown:   maxUnknown,
		Common:       q.Get("common") != "",
	}))
}
