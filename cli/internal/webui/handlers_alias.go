package webui

import (
	"net/http"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

type AliasesPageData struct {
	Base
	Aliases []mailbox.Alias
	Domains []mailbox.Domain
	Domain  string
	Query   string
}

func (s *Server) handleAliasesPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := AliasesPageData{
		Base:   s.base(w, r, "Aliases", "aliases", Crumb{Label: "Aliases"}),
		Domain: r.URL.Query().Get("domain"),
		Query:  strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))),
	}
	list, err := s.store.AliasList(ctx, data.Domain)
	if err != nil {
		failErr(w, r, err)
		return
	}
	for _, a := range list {
		if data.Query == "" || strings.Contains(a.Source, data.Query) || strings.Contains(a.Destination, data.Query) {
			data.Aliases = append(data.Aliases, a)
		}
	}
	if data.Domains, err = s.store.DomainList(ctx); err != nil {
		failErr(w, r, err)
		return
	}
	renderPage(w, "aliases", data)
}

func (s *Server) handleAliasAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, r, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	source := strings.ToLower(formValue(r, "local") + "@" + formValue(r, "domain"))
	dest := strings.ToLower(formValue(r, "destination"))
	if err := s.store.AliasAdd(r.Context(), source, dest); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "alias.add", source, "→ "+dest)
	done(w, r, "/aliases", source+" now delivers to "+dest+".")
}

func (s *Server) handleAliasDelete(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	source, dest := q.Get("source"), q.Get("destination")
	if err := s.store.AliasRemove(r.Context(), source, dest); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "alias.remove", source, "→ "+dest)
	done(w, r, "/aliases", "Removed "+source+" → "+dest+".")
}
