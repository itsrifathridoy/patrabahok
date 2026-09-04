package webui

import (
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

type AliasesPageData struct {
	Base
	Aliases []mailbox.Alias
}

func (s *Server) aliasesData(r *http.Request) (AliasesPageData, error) {
	list, err := s.store.AliasList(r.Context(), "")
	if err != nil {
		return AliasesPageData{}, err
	}
	return AliasesPageData{
		Base:    Base{Title: "Aliases", Active: "aliases", Username: userFromContext(r).Username},
		Aliases: list,
	}, nil
}

func (s *Server) handleAliasesPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.aliasesData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "aliases", data)
}

func (s *Server) handleAliasAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if err := s.store.AliasAdd(r.Context(), r.FormValue("source"), r.FormValue("destination")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondAliasesTable(w, r)
}

func (s *Server) handleAliasDelete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if err := s.store.AliasRemove(r.Context(), q.Get("source"), q.Get("destination")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondAliasesTable(w, r)
}

func (s *Server) respondAliasesTable(w http.ResponseWriter, r *http.Request) {
	data, err := s.aliasesData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPartial(w, "aliases", "aliases_table", data)
}
