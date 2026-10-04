package web

import (
	"fmt"
	"net/http"
	"strings"

	"walletflow/internal/address"
	"walletflow/internal/ledger"
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
	s.render(w, r, "wallets", "Кошельки", d)
}

// walletsAdd takes lines like "address[, name[, kind]]".
func (s *Server) walletsAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	defKind := ledger.Kind(r.FormValue("kind"))
	if !defKind.Valid() {
		defKind = ledger.KindMine
	}
	defName := strings.TrimSpace(r.FormValue("name"))
	var d walletsData
	for line := range strings.Lines(r.FormValue("lines")) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.FieldsFunc(line, func(c rune) bool { return c == ',' || c == ';' || c == '\t' })
		fam, addr, err := address.Normalize(parts[0])
		if err != nil {
			d.Errors = append(d.Errors, fmt.Sprintf("%s: %v", line, err))
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
	}
	if d.Added > 0 {
		if err := s.store.Reclassify(ctx); err != nil {
			serverError(w, err)
			return
		}
	}
	s.renderWallets(w, r, d)
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
	if err := s.store.Reclassify(r.Context()); err != nil {
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
	if err := s.store.Reclassify(r.Context()); err != nil {
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
	if err := s.store.Reclassify(ctx); err != nil {
		serverError(w, err)
		return
	}
	if r.FormValue("from") == "graph" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.refreshInbox(w, r)
}
