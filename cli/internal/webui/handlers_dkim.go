package webui

import (
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

type DKIMPageData struct {
	Base
	Domains  []mailbox.Domain
	Selected string
	Records  string
}

func (s *Server) handleDKIMPage(w http.ResponseWriter, r *http.Request) {
	domains, err := s.store.DomainList(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	selected := r.URL.Query().Get("domain")
	if selected == "" && len(domains) > 0 {
		selected = domains[0].Name
	}

	var records string
	if selected != "" {
		records, _ = sysinfo.DNSRecords(selected)
	}

	renderPage(w, "dkim", DKIMPageData{
		Base:     Base{Title: "DKIM & DNS", Active: "dkim", Username: userFromContext(r).Username},
		Domains:  domains,
		Selected: selected,
		Records:  records,
	})
}
