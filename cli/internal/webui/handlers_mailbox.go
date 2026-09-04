package webui

import (
	"fmt"
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

type MailboxView struct {
	Email      string
	Enabled    bool
	QuotaHuman string
}

type MailboxesPageData struct {
	Base
	Domains   []mailbox.Domain
	Mailboxes []MailboxView
}

func humanizeBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f G", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.0f M", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f K", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func (s *Server) mailboxesData(r *http.Request) (MailboxesPageData, error) {
	domains, err := s.store.DomainList(r.Context())
	if err != nil {
		return MailboxesPageData{}, err
	}
	list, err := s.store.MailboxList(r.Context(), "")
	if err != nil {
		return MailboxesPageData{}, err
	}
	views := make([]MailboxView, 0, len(list))
	for _, m := range list {
		views = append(views, MailboxView{Email: m.Email, Enabled: m.Enabled, QuotaHuman: humanizeBytes(m.QuotaBytes)})
	}
	return MailboxesPageData{
		Base:      Base{Title: "Mailboxes", Active: "mailboxes", Username: userFromContext(r).Username},
		Domains:   domains,
		Mailboxes: views,
	}, nil
}

func (s *Server) handleMailboxesPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.mailboxesData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "mailboxes", data)
}

func (s *Server) handleMailboxAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email := r.FormValue("local") + "@" + r.FormValue("domain")
	quotaBytes, err := mailbox.ParseQuota(r.FormValue("quota"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.MailboxAdd(r.Context(), email, r.FormValue("password"), quotaBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondMailboxesTable(w, r)
}

func (s *Server) handleMailboxDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.MailboxRemove(r.Context(), r.PathValue("email")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondMailboxesTable(w, r)
}

func (s *Server) handleMailboxPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email := r.PathValue("email")
	if err := s.store.MailboxPasswd(r.Context(), email, r.FormValue("password")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondMailboxesTable(w, r)
}

func (s *Server) handleMailboxQuota(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	email := r.PathValue("email")
	quotaBytes, err := mailbox.ParseQuota(r.FormValue("quota"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.MailboxSetQuota(r.Context(), email, quotaBytes); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondMailboxesTable(w, r)
}

func (s *Server) respondMailboxesTable(w http.ResponseWriter, r *http.Request) {
	data, err := s.mailboxesData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPartial(w, "mailboxes", "mailboxes_table", data)
}
