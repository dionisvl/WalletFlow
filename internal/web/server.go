// Package web serves the UI: server-rendered pages with htmx, charts fed by JSON endpoints.
package web

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/dionisvl/WalletFlow/internal/chain"
	"github.com/dionisvl/WalletFlow/internal/config"
	"github.com/dionisvl/WalletFlow/internal/i18n"
	"github.com/dionisvl/WalletFlow/internal/store"
	"github.com/dionisvl/WalletFlow/internal/syncer"
)

//go:embed assets
var assetsFS embed.FS

// Server holds the app state shared by handlers.
type Server struct {
	ctx    context.Context // lives as long as the app; background syncs use it
	store  *store.Store
	sync   *syncer.Syncer
	cfg    config.Config
	chains []chain.Chain
	pages  map[i18n.Lang]map[string]*template.Template
	parts  map[i18n.Lang]*template.Template
}

// New builds the server. ctx bounds background work such as syncs.
func New(ctx context.Context, st *store.Store, cfg config.Config) (*Server, error) {
	s := &Server{ctx: ctx, store: st, cfg: cfg, chains: chain.All(cfg.IgnoreChains...),
		pages: map[i18n.Lang]map[string]*template.Template{}, parts: map[i18n.Lang]*template.Template{}}
	s.sync = syncer.New(st, s.chains, s.apiKeys, s.enabledChains)
	if cfg.MaxPerAddress >= 0 {
		s.sync.MaxPerAddress = cfg.MaxPerAddress
	}
	tfs, err := fs.Sub(assetsFS, "assets/templates")
	if err != nil {
		return nil, err
	}
	// Templates are parsed once per language with that language's "t".
	for _, lang := range i18n.Langs {
		fm := funcsFor(lang)
		if s.parts[lang], err = template.New("").Funcs(fm).ParseFS(tfs, "partials.html"); err != nil {
			return nil, err
		}
		s.pages[lang] = map[string]*template.Template{}
		for _, p := range []string{"wallets", "inbox", "transactions", "flows", "balances", "graph", "export", "settings"} {
			t, err := template.New("").Funcs(fm).ParseFS(tfs, "layout.html", "partials.html", p+".html")
			if err != nil {
				return nil, fmt.Errorf("template %s: %w", p, err)
			}
			s.pages[lang][p] = t
		}
	}
	return s, nil
}

// Handler returns the HTTP handler with all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assetsFS, "assets/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /lang", s.setLang)
	mux.HandleFunc("GET /wallets", s.walletsPage)
	mux.HandleFunc("POST /wallets", s.walletsAdd)
	mux.HandleFunc("GET /wallets/export", s.walletsExport)
	mux.HandleFunc("POST /wallets/{id}", s.walletUpdate)
	mux.HandleFunc("POST /wallets/{id}/delete", s.walletDelete)

	mux.HandleFunc("GET /inbox", s.inboxPage)
	mux.HandleFunc("GET /inbox/count", s.inboxCount)
	mux.HandleFunc("POST /transfers/{id}/category", s.setCategory)
	mux.HandleFunc("POST /transfers/{id}/comment", s.setComment)
	mux.HandleFunc("POST /transfers/bulk", s.bulkCategory)
	mux.HandleFunc("POST /counterparty", s.markCounterparty)
	mux.HandleFunc("POST /assets/{id}/spam", s.markSpam)

	mux.HandleFunc("GET /transactions", s.transactionsPage)
	mux.HandleFunc("GET /flows", s.flowsPage)
	mux.HandleFunc("GET /flows/data", s.flowsData)
	mux.HandleFunc("GET /balances", s.balancesPage)
	mux.HandleFunc("GET /balances/data", s.balancesData)
	mux.HandleFunc("GET /graph", s.graphPage)
	mux.HandleFunc("GET /graph/data", s.graphData)
	mux.HandleFunc("GET /export", s.exportPage)
	mux.HandleFunc("GET /export/file", s.exportFile)

	mux.HandleFunc("GET /settings", s.settingsPage)
	mux.HandleFunc("POST /settings", s.settingsSave)
	mux.HandleFunc("POST /rules/{id}/delete", s.ruleDelete)
	mux.HandleFunc("GET /backup", s.backup)

	mux.HandleFunc("POST /sync", s.syncStart)
	mux.HandleFunc("POST /sync/stop", s.syncStop)
	mux.HandleFunc("GET /sync/status", s.syncStatus)
	return localOnly(recoverPanics(mux))
}

func recoverPanics(h http.Handler) http.Handler {
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

// localOnly blocks DNS rebinding (foreign Host) and cross-site form posts (foreign Origin).
func localOnly(h http.Handler) http.Handler {
	isLocal := func(host string) bool {
		if hst, _, err := net.SplitHostPort(host); err == nil {
			host = hst
		}
		return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocal(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				if err != nil || !isLocal(u.Host) {
					http.Error(w, "forbidden origin", http.StatusForbidden)
					return
				}
			}
		}
		h.ServeHTTP(w, r)
	})
}

const langCookie = "walletflow_lang"

// lang is the UI language of the request: the cookie, or English.
func lang(r *http.Request) i18n.Lang {
	if c, err := r.Cookie(langCookie); err == nil {
		return i18n.Parse(c.Value)
	}
	return i18n.EN
}

// tr returns the translator for the request language.
func tr(r *http.Request) func(string, ...any) string { return lang(r).T }

func (s *Server) setLang(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: langCookie, Value: string(i18n.Parse(r.URL.Query().Get("set"))),
		Path: "/", MaxAge: 10 * 365 * 24 * 3600, SameSite: http.SameSiteLaxMode})
	back := "/"
	if b := r.URL.Query().Get("back"); strings.HasPrefix(b, "/") && !strings.HasPrefix(b, "//") {
		back = b // explicit local path, e.g. a link that opens a page in a given language
	} else if u, err := url.Parse(r.Referer()); err == nil && u.Host == r.Host && u.Path != "/lang" {
		back = u.RequestURI()
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) page(r *http.Request, active, title string, data any) (Page, error) {
	ctx := r.Context()
	l := lang(r)
	book, err := s.store.Book(ctx)
	if err != nil {
		return Page{}, err
	}
	n, err := s.store.CountTransfers(ctx, store.Filter{Inbox: true})
	if err != nil {
		return Page{}, err
	}
	return Page{
		Title: l.T(title), Active: active, InboxCount: n, Book: book, Data: data,
		Categories: s.categories(ctx, l), Sync: s.sync.Status(), Query: r.URL.Query(), Chains: s.chains,
		Lang: l, Langs: i18n.Langs, JSDict: l.Dict(), Demo: s.cfg.Demo,
	}, nil
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	p, err := s.page(r, name, title, data)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pages[p.Lang][name].ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

func (s *Server) renderPart(w http.ResponseWriter, r *http.Request, name string, data any) {
	p, err := s.page(r, "", "", data)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.parts[p.Lang].ExecuteTemplate(w, name, p); err != nil {
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

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	addrs, err := s.store.Addresses(r.Context())
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

// defaultCategories are used until the user edits the list in Settings.
var defaultCategories = []string{"Purchase", "P2P sale", "Income", "Payment", "Gift", "Swap", "Refund", "Other"}

func defaultCategoriesText(l i18n.Lang) string {
	out := make([]string, len(defaultCategories))
	for i, c := range defaultCategories {
		out[i] = l.T(c)
	}
	return strings.Join(out, "\n")
}

func (s *Server) categories(ctx context.Context, l i18n.Lang) []string {
	var out []string
	for line := range strings.Lines(s.store.Setting(ctx, "categories", defaultCategoriesText(l))) {
		if c := strings.TrimSpace(line); c != "" {
			out = append(out, c)
		}
	}
	return out
}
