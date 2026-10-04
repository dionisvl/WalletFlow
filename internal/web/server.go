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

	"walletflow/internal/chain"
	"walletflow/internal/config"
	"walletflow/internal/store"
	"walletflow/internal/syncer"
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
	pages  map[string]*template.Template
	parts  *template.Template
}

// New builds the server. ctx bounds background work such as syncs.
func New(ctx context.Context, st *store.Store, cfg config.Config) (*Server, error) {
	s := &Server{ctx: ctx, store: st, cfg: cfg, chains: chain.All(cfg.IgnoreChains...), pages: map[string]*template.Template{}}
	s.sync = syncer.New(st, s.chains, s.apiKeys, s.enabledChains)
	tfs, err := fs.Sub(assetsFS, "assets/templates")
	if err != nil {
		return nil, err
	}
	s.parts, err = template.New("").Funcs(funcs).ParseFS(tfs, "partials.html")
	if err != nil {
		return nil, err
	}
	for _, p := range []string{"wallets", "inbox", "transactions", "flows", "balances", "graph", "export", "settings"} {
		t, err := template.New("").Funcs(funcs).ParseFS(tfs, "layout.html", "partials.html", p+".html")
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", p, err)
		}
		s.pages[p] = t
	}
	return s, nil
}

// Handler returns the HTTP handler with all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(assetsFS, "assets/static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))

	mux.HandleFunc("GET /{$}", s.home)
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

func (s *Server) page(r *http.Request, active, title string, data any) (Page, error) {
	ctx := r.Context()
	book, err := s.store.Book(ctx)
	if err != nil {
		return Page{}, err
	}
	n, err := s.store.CountTransfers(ctx, store.Filter{Inbox: true})
	if err != nil {
		return Page{}, err
	}
	return Page{
		Title: title, Active: active, InboxCount: n, Book: book, Data: data,
		Categories: s.categories(ctx), Sync: s.sync.Status(), Query: r.URL.Query(), Chains: s.chains,
	}, nil
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name, title string, data any) {
	p, err := s.page(r, name, title, data)
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
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
	if err := s.parts.ExecuteTemplate(w, name, p); err != nil {
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

const defaultCategories = "Покупка\nПродажа P2P\nДоход\nОплата\nПодарок\nСвоп\nВозврат\nДругое"

func (s *Server) categories(ctx context.Context) []string {
	var out []string
	for line := range strings.Lines(s.store.Setting(ctx, "categories", defaultCategories)) {
		if c := strings.TrimSpace(line); c != "" {
			out = append(out, c)
		}
	}
	return out
}
