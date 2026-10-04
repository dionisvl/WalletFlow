package web

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"walletflow/internal/export"
	"walletflow/internal/ledger"
)

// defaultChains are enabled until the user picks their own.
// Base, Optimism and BNB Chain need a paid Etherscan plan.
var defaultChains = []string{"ethereum", "arbitrum", "polygon", "linea", "tron"}

// apiKeys prefers keys from the environment (.env) over the ones saved in settings.
func (s *Server) apiKeys(ctx context.Context) (etherscan, trongrid string) {
	etherscan, trongrid = s.cfg.EtherscanKey, s.cfg.TronGridKey
	if etherscan == "" {
		etherscan = s.store.Setting(ctx, "etherscan_key", "")
	}
	if trongrid == "" {
		trongrid = s.store.Setting(ctx, "trongrid_key", "")
	}
	return etherscan, trongrid
}

func (s *Server) enabledChains(ctx context.Context) map[string]bool {
	m := map[string]bool{}
	for k := range strings.SplitSeq(s.store.Setting(ctx, "chains", strings.Join(defaultChains, ",")), ",") {
		if k != "" && !slices.Contains(s.cfg.IgnoreChains, k) {
			m[k] = true
		}
	}
	return m
}

type settingsData struct {
	EtherscanKey     string
	TronGridKey      string
	EtherscanFromEnv bool
	TronGridFromEnv  bool
	EnvFile          string
	IgnoreChains     []string
	Enabled          map[string]bool
	Categories       string
	Rules            []ledger.Rule
	DBPath           string
	Saved            bool
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, false)
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, saved bool) {
	ctx := r.Context()
	rules, err := s.store.Rules(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	s.render(w, r, "settings", "Настройки", settingsData{
		EtherscanKey:     s.store.Setting(ctx, "etherscan_key", ""),
		TronGridKey:      s.store.Setting(ctx, "trongrid_key", ""),
		EtherscanFromEnv: s.cfg.EtherscanKey != "",
		TronGridFromEnv:  s.cfg.TronGridKey != "",
		EnvFile:          s.cfg.EnvFile,
		IgnoreChains:     s.cfg.IgnoreChains,
		Enabled:          s.enabledChains(ctx),
		Categories:       s.store.Setting(ctx, "categories", defaultCategories),
		Rules:            rules,
		DBPath:           s.cfg.DBPath,
		Saved:            saved,
	})
}

func (s *Server) settingsSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.ParseForm()
	vals := map[string]string{
		"chains":     strings.Join(r.Form["chain"], ","),
		"categories": strings.TrimSpace(r.FormValue("categories")),
	}
	// Keys set in .env are shown read-only and never overwritten here.
	if s.cfg.EtherscanKey == "" {
		vals["etherscan_key"] = strings.TrimSpace(r.FormValue("etherscan_key"))
	}
	if s.cfg.TronGridKey == "" {
		vals["trongrid_key"] = strings.TrimSpace(r.FormValue("trongrid_key"))
	}
	for k, v := range vals {
		if err := s.store.SetSetting(ctx, k, v); err != nil {
			serverError(w, err)
			return
		}
	}
	s.renderSettings(w, r, true)
}

func (s *Server) ruleDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRule(r.Context(), pathID(r)); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// backup streams a consistent copy of the database.
func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	dir, err := os.MkdirTemp("", "walletflow-backup")
	if err != nil {
		serverError(w, err)
		return
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "walletflow.db")
	if err := s.store.Backup(r.Context(), p); err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="walletflow-`+time.Now().Format("2006-01-02")+`.db"`)
	http.ServeFile(w, r, p)
}

func (s *Server) exportPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, "export", "Экспорт", nil)
}

func (s *Server) exportFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	f, err := s.filterFromQuery(ctx, q)
	if err != nil {
		serverError(w, err)
		return
	}
	f.HideZero = false // the journal keeps fee-only rows
	ts, err := s.store.Transfers(ctx, f)
	if err != nil {
		serverError(w, err)
		return
	}
	addrs, err := s.store.Addresses(ctx)
	if err != nil {
		serverError(w, err)
		return
	}
	name := "walletflow-" + time.Now().Format("2006-01-02")
	if q.Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.csv"`)
		if err := export.CSV(w, ts, addrs); err != nil {
			log.Printf("export csv: %v", err)
		}
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.xlsx"`)
	if err := export.XLSX(w, ts, addrs); err != nil {
		log.Printf("export xlsx: %v", err)
	}
}

func (s *Server) syncStart(w http.ResponseWriter, r *http.Request) {
	s.sync.Start(s.ctx)
	s.renderPart(w, r, "sync-status", nil)
}

func (s *Server) syncStop(w http.ResponseWriter, r *http.Request) {
	s.sync.Stop()
	s.renderPart(w, r, "sync-status", nil)
}

func (s *Server) syncStatus(w http.ResponseWriter, r *http.Request) {
	if !s.sync.Status().Running {
		w.Header().Set("HX-Trigger", "inboxChanged")
	}
	s.renderPart(w, r, "sync-status", nil)
}
