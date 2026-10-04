package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dionisvl/WalletFlow/internal/address"
	"github.com/dionisvl/WalletFlow/internal/ledger"
)

type walletsData struct {
	Addresses []ledger.Address
	Errors    []string
	Added     int
}

func (s *Server) walletsPage(w http.ResponseWriter, r *http.Request) {
	s.renderWallets(w, r, walletsData{})
}

func (s *Server) renderWallets(w http.ResponseWriter, r *http.Request, d walletsData) {
	var err error
	if d.Addresses, err = s.store.Addresses(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	s.render(w, r, "wallets", "Wallets", d)
}

// walletsAdd takes one address from the form fields, or lines like
// "address[, name[, kind]]" where name and kind default to the form fields.
// With then=graph it starts a sync and opens the graph of the new wallets.
func (s *Server) walletsAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	defKind := ledger.Kind(r.FormValue("kind"))
	if !defKind.Valid() {
		defKind = ledger.KindMine
	}
	defName := strings.TrimSpace(r.FormValue("name"))
	lines := r.FormValue("lines")
	if a := strings.TrimSpace(r.FormValue("address")); a != "" {
		lines = a // name and kind come from the form fields
	}
	var d walletsData
	synced := false
	for line := range strings.Lines(lines) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := splitLine(line)
		fam, addr, err := address.Normalize(parts[0])
		if err != nil {
			d.Errors = append(d.Errors, line+": "+lang(r).T(err.Error()))
			continue
		}
		a := ledger.Address{Family: fam, Address: addr, Name: defName, Kind: defKind}
		if len(parts) > 1 {
			a.Name = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			if k := ledger.ParseKind(parts[2]); k != "" {
				a.Kind = k
			}
		}
		if err := s.store.UpsertAddress(ctx, a); err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", line, err))
			continue
		}
		d.Added++
		synced = synced || a.Kind.Synced()
	}
	if d.Added > 0 {
		if err := s.store.RefreshBook(ctx); err != nil {
			serverError(w, err)
			return
		}
	}
	if r.FormValue("then") == "graph" && len(d.Errors) == 0 {
		if synced && !s.cfg.Demo {
			s.sync.Start(s.ctx, tr(r))
		}
		http.Redirect(w, r, "/graph?common=1", http.StatusSeeOther)
		return
	}
	s.renderWallets(w, r, d)
}

// walletsExport downloads the address book in the format the import accepts.
func (s *Server) walletsExport(w http.ResponseWriter, r *http.Request) {
	addrs, err := s.store.Addresses(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="walletflow-wallets-`+time.Now().Format("2006-01-02")+`.txt"`)
	fmt.Fprintln(w, "# "+lang(r).T("address, name, kind")+" (mine / exchange / external / watch)")
	for _, a := range addrs {
		name := strings.NewReplacer(",", " ", ";", " ", "\t", " ").Replace(a.Name)
		fmt.Fprintf(w, "%s, %s, %s\n", a.Address, name, a.Kind)
	}
}

func (s *Server) walletUpdate(w http.ResponseWriter, r *http.Request) {
	k := ledger.Kind(r.FormValue("kind"))
	if !k.Valid() {
		http.Error(w, "bad kind", http.StatusBadRequest)
		return
	}
	a := ledger.Address{ID: pathID(r), Name: strings.TrimSpace(r.FormValue("name")), Kind: k, Color: r.FormValue("color")}
	if err := s.store.UpdateAddress(r.Context(), a); err != nil {
		serverError(w, err)
		return
	}
	if err := s.store.RefreshBook(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("HX-Trigger", "inboxChanged")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) walletDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAddress(r.Context(), pathID(r)); err != nil {
		serverError(w, err)
		return
	}
	if err := s.store.RefreshBook(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("HX-Trigger", "inboxChanged")
	w.WriteHeader(http.StatusOK) // empty body removes the row
}

// markCounterparty adds the counterparty of a transfer to the address book.
func (s *Server) markCounterparty(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	fam, addr, err := address.Normalize(r.FormValue("address"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	k := ledger.Kind(r.FormValue("kind"))
	if !k.Valid() {
		k = ledger.KindExternal
	}
	if err := s.store.UpsertAddress(ctx, ledger.Address{Family: fam, Address: addr, Name: strings.TrimSpace(r.FormValue("name")), Kind: k}); err != nil {
		serverError(w, err)
		return
	}
	if err := s.store.RefreshBook(ctx); err != nil {
		serverError(w, err)
		return
	}
	if r.FormValue("from") == "graph" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.refreshInbox(w, r)
}

// splitLine splits "a, b, c" on commas, semicolons or tabs, keeping empty fields.
func splitLine(line string) []string {
	parts := strings.Split(strings.NewReplacer(";", ",", "\t", ",").Replace(line), ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}
