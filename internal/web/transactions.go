package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"walletflow/internal/ledger"
	"walletflow/internal/store"
)

const txPageSize = 100

type txRow struct {
	ledger.Transfer
	SameTx bool // same tx_hash as the previous row
}

type txData struct {
	Rows      []txRow
	Total     int
	Page      int
	Pages     int
	Assets    []ledger.Asset
	Wallets   []string
	PrevQuery string
	NextQuery string
}

// parseDate reads YYYY-MM-DD as UTC; end=true moves to the next day.
func parseDate(s string, end bool) int64 {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0
	}
	if end {
		t = t.AddDate(0, 0, 1)
	}
	return t.Unix()
}

// resolve turns a wallet name (or a raw address) into addresses.
func resolve(book ledger.Book, ref string) []string {
	var out []string
	for _, a := range book {
		if a.Label() == ref {
			out = append(out, a.Address)
		}
	}
	if len(out) == 0 {
		out = []string{ref}
	}
	return out
}

// filterFromQuery builds a store.Filter from the common query parameters.
func (s *Server) filterFromQuery(ctx context.Context, q url.Values) (store.Filter, error) {
	f := store.Filter{
		From:     parseDate(q.Get("from"), false),
		To:       parseDate(q.Get("to"), true),
		Chain:    q.Get("chain"),
		Class:    ledger.Class(q.Get("class")),
		HideSpam: q.Get("spam") == "",
		HideZero: q.Get("zero") == "",
	}
	f.AssetID, _ = strconv.ParseInt(q.Get("asset"), 10, 64)
	// Watched wallets' own history shows only when asked for: by class, wallet, link or the checkbox.
	f.OnlyLedger = q.Get("all") == "" && f.Class == "" && q.Get("wallet") == "" && q.Get("src") == "" && q.Get("dst") == ""
	switch c := q.Get("category"); c {
	case "":
	case "-":
		f.Uncategorized = true
	default:
		f.Category = c
	}
	if q.Get("wallet") == "" && q.Get("src") == "" && q.Get("dst") == "" {
		return f, nil
	}
	book, err := s.store.Book(ctx)
	if err != nil {
		return f, err
	}
	if v := q.Get("wallet"); v != "" {
		f.Addresses = resolve(book, v)
	}
	if v := q.Get("src"); v != "" {
		f.FromAddrs = resolve(book, v)
	}
	if v := q.Get("dst"); v != "" {
		f.ToAddrs = resolve(book, v)
	}
	return f, nil
}

func walletNames(addrs []ledger.Address) []string {
	var names []string
	for _, a := range addrs {
		if a.Kind != ledger.KindExternal && !slices.Contains(names, a.Label()) {
			names = append(names, a.Label())
		}
	}
	slices.Sort(names)
	return names
}

func (s *Server) transactionsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f, err := s.filterFromQuery(ctx, q)
	if err != nil {
		serverError(w, err)
		return
	}
	var d txData
	if d.Total, err = s.store.CountTransfers(ctx, f); err != nil {
		serverError(w, err)
		return
	}
	d.Page, _ = strconv.Atoi(q.Get("page"))
	d.Pages = max(1, (d.Total+txPageSize-1)/txPageSize)
	d.Page = min(max(d.Page, 1), d.Pages)
	f.Limit, f.Offset = txPageSize, (d.Page-1)*txPageSize
	list, err := s.store.Transfers(ctx, f)
	if err != nil {
		serverError(w, err)
		return
	}
	for i, t := range list {
		d.Rows = append(d.Rows, txRow{Transfer: t, SameTx: i > 0 && list[i-1].TxHash == t.TxHash})
	}
	if d.Assets, err = s.store.Assets(ctx); err != nil {
		serverError(w, err)
		return
	}
	addrs, err := s.store.Addresses(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	d.Wallets = walletNames(addrs)
	pageQuery := func(p int) string {
		c := url.Values{}
		for k, v := range q {
			c[k] = v
		}
		c.Set("page", strconv.Itoa(p))
		return c.Encode()
	}
	if d.Page > 1 {
		d.PrevQuery = pageQuery(d.Page - 1)
	}
	if d.Page < d.Pages {
		d.NextQuery = pageQuery(d.Page + 1)
	}
	s.render(w, r, "transactions", "Транзакции", d)
}
