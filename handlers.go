package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

var defaultCategories = "Покупка\nПродажа P2P\nДоход\nОплата\nПодарок\nСвоп\nВозврат\nДругое"

type App struct {
	ctx    context.Context
	store  *Store
	sync   *Syncer
	dbPath string
	pages  map[string]*template.Template
	parts  *template.Template
}

// Page is the data every template gets.
type Page struct {
	Title      string
	Active     string
	InboxCount int
	Categories []string
	Sync       SyncStatus
	Book       map[string]Address
	Data       any
	Query      url.Values
}

// Label returns the address book name of addr, or its short form.
func (p Page) Label(addr string) string {
	if a, ok := p.Book[addr]; ok && a.Name != "" {
		return a.Name
	}
	return shortAddr(addr)
}

func (p Page) KindOf(addr string) Kind {
	if a, ok := p.Book[addr]; ok {
		return a.Kind
	}
	return ""
}

func (p Page) Known(addr string) bool {
	_, ok := p.Book[addr]
	return ok
}

// Fee formats the fee of t when the owner paid it.
func (p Page) Fee(t Transfer) string {
	if t.FeeRaw == "" || t.FeeRaw == "0" || p.KindOf(t.From) != KindMine {
		return ""
	}
	c, _ := chainByKey(t.Chain)
	return formatAmount(t.FeeRaw, c.Decimals) + " " + c.Symbol
}

func (p Page) Q(key string) string { return p.Query.Get(key) }

// Lookalike returns the name of a known address that addr imitates:
// same first and last 4 characters but a different address (address poisoning).
func (p Page) Lookalike(addr string) string {
	if p.Known(addr) || len(addr) < 12 {
		return ""
	}
	start := 2 // skip 0x
	if !strings.HasPrefix(addr, "0x") {
		start = 1 // skip T
	}
	for _, a := range p.Book {
		k := a.Address
		if len(k) == len(addr) && k[start:start+4] == addr[start:start+4] && k[len(k)-4:] == addr[len(addr)-4:] {
			return a.Label()
		}
	}
	return ""
}

var kindLabels = map[Kind]string{KindMine: "Мой", KindExchange: "Биржа", KindExternal: "Чужой"}

var classLabels = map[Class]string{
	ClassInternal:      "Внутренний",
	ClassCEXDeposit:    "На биржу",
	ClassCEXWithdrawal: "С биржи",
	ClassInflow:        "Входящий",
	ClassOutflow:       "Исходящий",
	ClassUnknown:       "Неизвестно",
}

var classes = []Class{ClassInflow, ClassOutflow, ClassInternal, ClassCEXDeposit, ClassCEXWithdrawal, ClassUnknown}

var funcs = template.FuncMap{
	"short":     shortAddr,
	"explorer":  explorerURL,
	"chainName": chainName,
	"kindLabel": func(k Kind) string { return kindLabels[k] },
	"classLabel": func(c Class) string {
		return classLabels[c]
	},
	"date":    func(t time.Time) string { return t.Format("2006-01-02 15:04") },
	"add1":    func(i int) int { return i + 1 },
	"kinds":   func() []Kind { return kinds },
	"classes": func() []Class { return classes },
	"chains":  func() []Chain { return chains },
	"amount":  formatAmount,
	"inList":  func(s string, list []string) bool { return slices.Contains(list, s) },
	"pair":    func(p Page, t Transfer) rowData { return rowData{p, t} },
}

// rowData lets a row partial see both the page and its transfer.
type rowData struct {
	P Page
	T Transfer
}

func newApp(ctx context.Context, st *Store, dbPath string) (*App, error) {
	a := &App{ctx: ctx, store: st, sync: newSyncer(st), dbPath: dbPath, pages: map[string]*template.Template{}}
	tfs, err := fs.Sub(webFS, "web/templates")
	if err != nil {
		return nil, err
	}
	a.parts, err = template.New("").Funcs(funcs).ParseFS(tfs, "partials.html")
	if err != nil {
		return nil, err
	}
	for _, p := range []string{"wallets", "inbox", "transactions", "flows", "balances", "export", "settings"} {
		t, err := template.New("").Funcs(funcs).ParseFS(tfs, "layout.html", "partials.html", p+".html")
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", p, err)
		}
		a.pages[p] = t
	}
	return a, nil
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /wallets", a.walletsPage)
	mux.HandleFunc("POST /wallets", a.walletsAdd)
	mux.HandleFunc("POST /wallets/{id}", a.walletUpdate)
	mux.HandleFunc("POST /wallets/{id}/delete", a.walletDelete)

	mux.HandleFunc("GET /inbox", a.inboxPage)
	mux.HandleFunc("GET /inbox/count", a.inboxCount)
	mux.HandleFunc("POST /transfers/{id}/category", a.setCategory)
	mux.HandleFunc("POST /transfers/{id}/comment", a.setComment)
	mux.HandleFunc("POST /transfers/bulk", a.bulkCategory)
	mux.HandleFunc("POST /counterparty", a.markCounterparty)
	mux.HandleFunc("POST /assets/{id}/spam", a.markSpam)

	mux.HandleFunc("GET /transactions", a.transactionsPage)
	mux.HandleFunc("GET /flows", a.flowsPage)
	mux.HandleFunc("GET /flows/data", a.flowsData)
	mux.HandleFunc("GET /balances", a.balancesPage)
	mux.HandleFunc("GET /balances/data", a.balancesData)
	mux.HandleFunc("GET /export", a.exportPage)
	mux.HandleFunc("GET /export/file", a.exportFile)

	mux.HandleFunc("GET /settings", a.settingsPage)
	mux.HandleFunc("POST /settings", a.settingsSave)
	mux.HandleFunc("POST /rules/{id}/delete", a.ruleDelete)
	mux.HandleFunc("GET /backup", a.backup)

	mux.HandleFunc("POST /sync", a.syncStart)
	mux.HandleFunc("GET /sync/status", a.syncStatus)
	mux.HandleFunc("POST /sync/stop", a.syncStop)
	return logErrors(mux)
}

func logErrors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("panic %s %s: %v", r.Method, r.URL.Path, v)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		h.ServeHTTP(w, r)
	})
}

func (a *App) categories(ctx context.Context) []string {
	var out []string
	for line := range strings.Lines(a.store.Setting(ctx, "categories", defaultCategories)) {
		if s := strings.TrimSpace(line); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (a *App) page(r *http.Request, active, title string, data any) (Page, error) {
	ctx := r.Context()
	book, err := a.store.AddressBook(ctx)
	if err != nil {
		return Page{}, err
	}
	n, err := a.store.CountTransfers(ctx, Filter{Inbox: true})
	if err != nil {
		return Page{}, err
	}
	return Page{
		Title: title, Active: active, InboxCount: n, Book: book, Data: data,
		Categories: a.categories(ctx), Sync: a.sync.Status(), Query: r.URL.Query(),
	}, nil
}

func (a *App) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	p, err := a.page(r, name, title, data)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (a *App) renderPart(w http.ResponseWriter, r *http.Request, name string, data any) {
	p, err := a.page(r, "", "", data)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.parts.ExecuteTemplate(w, name, p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func serverError(w http.ResponseWriter, err error) {
	log.Print(err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	addrs, err := a.store.Addresses(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	if len(addrs) == 0 {
		http.Redirect(w, r, "/wallets", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/inbox", http.StatusSeeOther)
}

// --- wallets ---

type walletsData struct {
	Addresses []Address
	Errors    []string
	Added     int
}

func (a *App) walletsPage(w http.ResponseWriter, r *http.Request) {
	a.renderWallets(w, r, walletsData{})
}

func (a *App) renderWallets(w http.ResponseWriter, r *http.Request, d walletsData) {
	var err error
	if d.Addresses, err = a.store.Addresses(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	a.render(w, r, "wallets", "Кошельки", d)
}

// walletsAdd takes lines like "address[, name[, kind]]".
func (a *App) walletsAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	defKind := r.FormValue("kind")
	if !validKind(defKind) {
		defKind = string(KindMine)
	}
	defName := strings.TrimSpace(r.FormValue("name"))
	var d walletsData
	for line := range strings.Lines(r.FormValue("lines")) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.FieldsFunc(line, func(c rune) bool { return c == ',' || c == ';' || c == '\t' })
		fam, addr, err := normalizeAddress(parts[0])
		if err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", line, err))
			continue
		}
		ad := Address{Family: fam, Address: addr, Name: defName, Kind: Kind(defKind)}
		if len(parts) > 1 {
			ad.Name = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			if k := parseKind(parts[2]); k != "" {
				ad.Kind = k
			}
		}
		if err := a.store.UpsertAddress(ctx, ad); err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", line, err))
			continue
		}
		d.Added++
	}
	if d.Added > 0 {
		if err := a.store.Reclassify(ctx); err != nil {
			serverError(w, err)
			return
		}
	}
	a.renderWallets(w, r, d)
}

// parseKind accepts English and Russian names.
func parseKind(s string) Kind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "mine", "my", "мой", "мои", "свой":
		return KindMine
	case "exchange", "cex", "биржа", "биржевой":
		return KindExchange
	case "external", "other", "чужой", "чужие":
		return KindExternal
	}
	return ""
}

func (a *App) walletUpdate(w http.ResponseWriter, r *http.Request) {
	k := r.FormValue("kind")
	if !validKind(k) {
		http.Error(w, "bad kind", http.StatusBadRequest)
		return
	}
	ad := Address{ID: pathID(r), Name: strings.TrimSpace(r.FormValue("name")), Kind: Kind(k), Color: r.FormValue("color")}
	if err := a.store.UpdateAddress(r.Context(), ad); err != nil {
		serverError(w, err)
		return
	}
	if err := a.store.Reclassify(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("HX-Trigger", "inboxChanged")
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) walletDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteAddress(r.Context(), pathID(r)); err != nil {
		serverError(w, err)
		return
	}
	if err := a.store.Reclassify(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("HX-Trigger", "inboxChanged")
	w.WriteHeader(http.StatusOK) // empty body removes the row
}

// --- inbox ---

const inboxPageSize = 50

type inboxData struct {
	Items []Transfer
	Total int
}

func (a *App) inboxItems(ctx context.Context) (inboxData, error) {
	f := Filter{Inbox: true}
	total, err := a.store.CountTransfers(ctx, f)
	if err != nil {
		return inboxData{}, err
	}
	f.Limit = inboxPageSize
	items, err := a.store.Transfers(ctx, f)
	return inboxData{Items: items, Total: total}, err
}

func (a *App) inboxPage(w http.ResponseWriter, r *http.Request) {
	d, err := a.inboxItems(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	a.render(w, r, "inbox", "Inbox", d)
}

// refreshInbox re-renders the whole list in place of the clicked row.
func (a *App) refreshInbox(w http.ResponseWriter, r *http.Request) {
	d, err := a.inboxItems(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("HX-Retarget", "#inbox-list")
	w.Header().Set("HX-Reswap", "outerHTML")
	w.Header().Set("HX-Trigger", "inboxChanged")
	a.renderPart(w, r, "inbox-list", d)
}

func (a *App) inboxCount(w http.ResponseWriter, r *http.Request) {
	n, err := a.store.CountTransfers(r.Context(), Filter{Inbox: true})
	if err != nil {
		serverError(w, err)
		return
	}
	if n > 0 {
		fmt.Fprint(w, n)
	}
}

func (a *App) setCategory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathID(r)
	cat := strings.TrimSpace(r.FormValue("category"))
	if err := a.store.SetCategory(ctx, []int64{id}, cat); err != nil {
		serverError(w, err)
		return
	}
	if r.FormValue("rule") != "" && cat != "" {
		t, err := a.store.Transfer(ctx, id)
		if err != nil {
			serverError(w, err)
			return
		}
		if err := a.store.AddRule(ctx, Rule{Counterparty: t.Counterparty(), Class: t.Class, Category: cat}); err != nil {
			serverError(w, err)
			return
		}
		if r.FormValue("from") == "inbox" {
			a.refreshInbox(w, r)
			return
		}
	}
	w.Header().Set("HX-Trigger", "inboxChanged")
	if r.FormValue("from") == "inbox" {
		w.WriteHeader(http.StatusOK) // empty body removes the row
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) setComment(w http.ResponseWriter, r *http.Request) {
	if err := a.store.SetComment(r.Context(), pathID(r), strings.TrimSpace(r.FormValue("comment"))); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) bulkCategory(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var ids []int64
	for _, s := range r.Form["id"] {
		if id, err := strconv.ParseInt(s, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if err := a.store.SetCategory(r.Context(), ids, strings.TrimSpace(r.FormValue("category"))); err != nil {
		serverError(w, err)
		return
	}
	a.refreshInbox(w, r)
}

// markCounterparty adds the counterparty of a transfer to the address book.
func (a *App) markCounterparty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fam, addr, err := normalizeAddress(r.FormValue("address"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	k := Kind(r.FormValue("kind"))
	if !validKind(string(k)) {
		k = KindExternal
	}
	if err := a.store.UpsertAddress(ctx, Address{Family: fam, Address: addr, Name: strings.TrimSpace(r.FormValue("name")), Kind: k}); err != nil {
		serverError(w, err)
		return
	}
	if err := a.store.Reclassify(ctx); err != nil {
		serverError(w, err)
		return
	}
	a.refreshInbox(w, r)
}

func (a *App) markSpam(w http.ResponseWriter, r *http.Request) {
	spam := r.FormValue("spam") != "0"
	if err := a.store.SetAssetSpam(r.Context(), pathID(r), spam); err != nil {
		serverError(w, err)
		return
	}
	if r.FormValue("from") == "inbox" {
		a.refreshInbox(w, r)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}

// --- transactions ---

const txPageSize = 100

type txRow struct {
	Transfer
	SameTx bool // same tx_hash as the previous row
}

type txData struct {
	Rows      []txRow
	Total     int
	Page      int
	Pages     int
	Assets    []Asset
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

// filterFromQuery builds a Filter from the common query parameters.
func (a *App) filterFromQuery(ctx context.Context, q url.Values) (Filter, error) {
	f := Filter{
		From:     parseDate(q.Get("from"), false),
		To:       parseDate(q.Get("to"), true),
		Chain:    q.Get("chain"),
		Class:    q.Get("class"),
		HideSpam: q.Get("spam") == "",
		HideZero: q.Get("zero") == "",
	}
	f.AssetID, _ = strconv.ParseInt(q.Get("asset"), 10, 64)
	switch c := q.Get("category"); c {
	case "":
	case "-":
		f.Uncategorized = true
	default:
		f.Category = c
	}
	if name := q.Get("wallet"); name != "" {
		addrs, err := a.store.Addresses(ctx)
		if err != nil {
			return f, err
		}
		for _, ad := range addrs {
			if ad.Label() == name {
				f.Addresses = append(f.Addresses, ad.Address)
			}
		}
		if len(f.Addresses) == 0 {
			f.Addresses = []string{name} // raw address
		}
	}
	return f, nil
}

func walletNames(addrs []Address) []string {
	var names []string
	for _, ad := range addrs {
		if ad.Kind != KindExternal && !slices.Contains(names, ad.Label()) {
			names = append(names, ad.Label())
		}
	}
	slices.Sort(names)
	return names
}

func (a *App) transactionsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f, err := a.filterFromQuery(ctx, q)
	if err != nil {
		serverError(w, err)
		return
	}
	var d txData
	if d.Total, err = a.store.CountTransfers(ctx, f); err != nil {
		serverError(w, err)
		return
	}
	d.Page, _ = strconv.Atoi(q.Get("page"))
	d.Pages = max(1, (d.Total+txPageSize-1)/txPageSize)
	d.Page = min(max(d.Page, 1), d.Pages)
	f.Limit, f.Offset = txPageSize, (d.Page-1)*txPageSize
	list, err := a.store.Transfers(ctx, f)
	if err != nil {
		serverError(w, err)
		return
	}
	for i, t := range list {
		d.Rows = append(d.Rows, txRow{Transfer: t, SameTx: i > 0 && list[i-1].TxHash == t.TxHash})
	}
	if d.Assets, err = a.store.Assets(ctx); err != nil {
		serverError(w, err)
		return
	}
	addrs, err := a.store.Addresses(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	d.Wallets = walletNames(addrs)
	pq := func(p int) string {
		c := url.Values{}
		for k, v := range q {
			c[k] = v
		}
		c.Set("page", strconv.Itoa(p))
		return c.Encode()
	}
	if d.Page > 1 {
		d.PrevQuery = pq(d.Page - 1)
	}
	if d.Page < d.Pages {
		d.NextQuery = pq(d.Page + 1)
	}
	a.render(w, r, "transactions", "Транзакции", d)
}

// --- flows ---

type flowsData struct {
	Assets []Asset
}

func (a *App) flowsPage(w http.ResponseWriter, r *http.Request) {
	assets, err := a.store.Assets(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var visible []Asset
	for _, as := range assets {
		if !as.IsSpam {
			visible = append(visible, as)
		}
	}
	a.render(w, r, "flows", "Потоки", flowsData{Assets: visible})
}

func (a *App) flowsData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, err := a.filterFromQuery(ctx, r.URL.Query())
	if err != nil {
		serverError(w, err)
		return
	}
	if f.AssetID == 0 {
		http.Error(w, "asset required", http.StatusBadRequest)
		return
	}
	ts, err := a.store.Transfers(ctx, f)
	if err != nil {
		serverError(w, err)
		return
	}
	book, err := a.store.AddressBook(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	minPct, _ := strconv.ParseFloat(r.URL.Query().Get("min"), 64)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildSankey(ts, book, minPct))
}

// --- balances ---

func (a *App) balancesPage(w http.ResponseWriter, r *http.Request) {
	assets, err := a.store.Assets(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	var visible []Asset
	for _, as := range assets {
		if !as.IsSpam {
			visible = append(visible, as)
		}
	}
	a.render(w, r, "balances", "Балансы", flowsData{Assets: visible})
}

func (a *App) balancesData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, _ := strconv.ParseInt(r.URL.Query().Get("asset"), 10, 64)
	if id == 0 {
		http.Error(w, "asset required", http.StatusBadRequest)
		return
	}
	ts, err := a.store.Transfers(ctx, Filter{AssetID: id})
	if err != nil {
		serverError(w, err)
		return
	}
	book, err := a.store.AddressBook(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(buildBalances(ts, book))
}

// --- export ---

func (a *App) exportPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "export", "Экспорт", nil)
}

func (a *App) exportFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f, err := a.filterFromQuery(ctx, q)
	if err != nil {
		serverError(w, err)
		return
	}
	ts, err := a.store.Transfers(ctx, f)
	if err != nil {
		serverError(w, err)
		return
	}
	addrs, err := a.store.Addresses(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	name := "walletflow-" + time.Now().Format("2006-01-02")
	if q.Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
		if err := writeCSV(w, ts, addrs); err != nil {
			log.Printf("export csv: %v", err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.xlsx"`)
	if err := writeXLSX(w, ts, addrs); err != nil {
		log.Printf("export xlsx: %v", err)
	}
}

// --- settings ---

type settingsData struct {
	EtherscanKey string
	TronGridKey  string
	Chains       map[string]bool
	Categories   string
	Rules        []Rule
	DBPath       string
	Saved        bool
}

func (a *App) settingsPage(w http.ResponseWriter, r *http.Request) {
	a.renderSettings(w, r, false)
}

func (a *App) renderSettings(w http.ResponseWriter, r *http.Request, saved bool) {
	ctx := r.Context()
	rules, err := a.store.Rules(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	a.render(w, r, "settings", "Настройки", settingsData{
		EtherscanKey: a.store.Setting(ctx, "etherscan_key", ""),
		TronGridKey:  a.store.Setting(ctx, "trongrid_key", ""),
		Chains:       enabledChains(ctx, a.store),
		Categories:   a.store.Setting(ctx, "categories", defaultCategories),
		Rules:        rules,
		DBPath:       a.dbPath,
		Saved:        saved,
	})
}

func (a *App) settingsSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	vals := map[string]string{
		"etherscan_key": strings.TrimSpace(r.FormValue("etherscan_key")),
		"trongrid_key":  strings.TrimSpace(r.FormValue("trongrid_key")),
		"chains":        strings.Join(r.Form["chain"], ","),
		"categories":    strings.TrimSpace(r.FormValue("categories")),
	}
	for k, v := range vals {
		if err := a.store.SetSetting(ctx, k, v); err != nil {
			serverError(w, err)
			return
		}
	}
	a.renderSettings(w, r, true)
}

func (a *App) ruleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteRule(r.Context(), pathID(r)); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// backup streams a consistent copy of the database.
func (a *App) backup(w http.ResponseWriter, r *http.Request) {
	dir, err := os.MkdirTemp("", "walletflow-backup")
	if err != nil {
		serverError(w, err)
		return
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "walletflow.db")
	if _, err := a.store.db.ExecContext(r.Context(), "VACUUM INTO ?", p); err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="walletflow-`+time.Now().Format("2006-01-02")+`.db"`)
	http.ServeFile(w, r, p)
}

// --- sync ---

func (a *App) syncStart(w http.ResponseWriter, r *http.Request) {
	a.sync.Start(a.ctx)
	a.renderPart(w, r, "sync-status", nil)
}

func (a *App) syncStop(w http.ResponseWriter, r *http.Request) {
	a.sync.Stop()
	a.renderPart(w, r, "sync-status", nil)
}

func (a *App) syncStatus(w http.ResponseWriter, r *http.Request) {
	if !a.sync.Status().Running {
		w.Header().Set("HX-Trigger", "inboxChanged")
	}
	a.renderPart(w, r, "sync-status", nil)
}
