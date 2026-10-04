package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dionisvl/WalletFlow/internal/ledger"
	"github.com/dionisvl/WalletFlow/internal/store"
)

const inboxPageSize = 50

type inboxData struct {
	Items []ledger.Transfer
	Total int
}

func (s *Server) inboxItems(ctx context.Context) (inboxData, error) {
	f := store.Filter{Inbox: true}
	total, err := s.store.CountTransfers(ctx, f)
	if err != nil {
		return inboxData{}, err
	}
	f.Limit = inboxPageSize
	items, err := s.store.Transfers(ctx, f)
	return inboxData{Items: items, Total: total}, err
}

func (s *Server) inboxPage(w http.ResponseWriter, r *http.Request) {
	d, err := s.inboxItems(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	s.render(w, r, "inbox", "Inbox", d)
}

// refreshInbox re-renders the whole list in place of the clicked row.
func (s *Server) refreshInbox(w http.ResponseWriter, r *http.Request) {
	d, err := s.inboxItems(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("HX-Retarget", "#inbox-list")
	w.Header().Set("HX-Reswap", "outerHTML")
	w.Header().Set("HX-Trigger", "inboxChanged")
	s.renderPart(w, r, "inbox-list", d)
}

func (s *Server) inboxCount(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountTransfers(r.Context(), store.Filter{Inbox: true})
	if err != nil {
		serverError(w, err)
		return
	}
	if n > 0 {
		fmt.Fprint(w, n)
	}
}

func (s *Server) setCategory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := pathID(r)
	cat := strings.TrimSpace(r.FormValue("category"))
	if err := s.store.SetCategory(ctx, []int64{id}, cat); err != nil {
		serverError(w, err)
		return
	}
	fromInbox := r.FormValue("from") == "inbox"
	if r.FormValue("rule") != "" && cat != "" {
		t, err := s.store.Transfer(ctx, id)
		if err != nil {
			serverError(w, err)
			return
		}
		if t.Class == ledger.ClassInflow || t.Class == ledger.ClassOutflow {
			if err := s.store.AddRule(ctx, ledger.Rule{Counterparty: t.Counterparty(), Class: t.Class, Category: cat}); err != nil {
				serverError(w, err)
				return
			}
			if fromInbox {
				s.refreshInbox(w, r)
				return
			}
		}
	}
	w.Header().Set("HX-Trigger", "inboxChanged")
	if fromInbox {
		w.WriteHeader(http.StatusOK) // empty body removes the row
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setComment(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SetComment(r.Context(), pathID(r), strings.TrimSpace(r.FormValue("comment"))); err != nil {
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) bulkCategory(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	var ids []int64
	for _, v := range r.Form["id"] {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	if err := s.store.SetCategory(r.Context(), ids, strings.TrimSpace(r.FormValue("category"))); err != nil {
		serverError(w, err)
		return
	}
	s.refreshInbox(w, r)
}

func (s *Server) markSpam(w http.ResponseWriter, r *http.Request) {
	spam := r.FormValue("spam") != "0"
	if err := s.store.SetAssetSpam(r.Context(), pathID(r), spam); err != nil {
		serverError(w, err)
		return
	}
	if r.FormValue("from") == "inbox" {
		s.refreshInbox(w, r)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusNoContent)
}
