package webui

import (
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

type DomainsPageData struct {
	Base
	Domains   []mailbox.Domain
	JustAdded string
}

func (s *Server) domainsData(r *http.Request) (DomainsPageData, error) {
	domains, err := s.store.DomainList(r.Context())
	if err != nil {
		return DomainsPageData{}, err
	}
	return DomainsPageData{
		Base:    Base{Title: "Domains", Active: "domains", Username: userFromContext(r).Username},
		Domains: domains,
	}, nil
}

func (s *Server) handleDomainsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.domainsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "domains", data)
}

func (s *Server) handleDomainAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if err := s.store.DomainAdd(r.Context(), name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := s.domainsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.JustAdded = name
	renderPartial(w, "domains", "domains_table", data)
}

func (s *Server) handleDomainDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DomainRemove(r.Context(), r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondDomainsTable(w, r)
}

func (s *Server) respondDomainsTable(w http.ResponseWriter, r *http.Request) {
	data, err := s.domainsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPartial(w, "domains", "domains_table", data)
}
