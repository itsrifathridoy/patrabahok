package webui

import (
	"net/http"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/audit"
)

type AuditView struct {
	audit.Entry
}

// ActorLabel is the actor without its kind prefix ("admin:alice" → "alice").
func (a AuditView) ActorLabel() string {
	if _, rest, ok := strings.Cut(a.Actor, ":"); ok {
		return rest
	}
	return a.Actor
}

// ActorDisplay is a short human label for lists: the admin's name, "Command line",
// "API key (school.edu)", or "Token backup-script".
func (a AuditView) ActorDisplay() string {
	switch a.ActorKind() {
	case "cli":
		return "Command line"
	case "apikey":
		return "API key (" + a.ActorLabel() + ")"
	case "token":
		return "Token " + a.ActorLabel()
	}
	return a.ActorLabel()
}

// ActorKind is "admin", "cli", "token" or "apikey".
func (a AuditView) ActorKind() string {
	kind, _, _ := strings.Cut(a.Actor, ":")
	return kind
}

// Verb is a readable form of the action ("group.members.add" → "group members add").
func (a AuditView) Verb() string { return strings.ReplaceAll(a.Action, ".", " ") }

func views(entries []audit.Entry) []AuditView {
	out := make([]AuditView, len(entries))
	for i, e := range entries {
		out[i] = AuditView{e}
	}
	return out
}

// recentActivity returns the latest audit entries mentioning target ("" for any).
func (s *Server) recentActivity(r *http.Request, target string, n int) []AuditView {
	entries, _, err := s.audit.List(r.Context(), audit.Filter{Query: target, Limit: n})
	if err != nil {
		return nil
	}
	return views(entries)
}

type AuditPageData struct {
	Base
	Entries []AuditView
	Pager   Pager
	Query   string
}

const auditPageSize = 50

func (s *Server) handleAuditPage(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	pager := newPager(r, auditPageSize, 0)
	entries, total, err := s.audit.List(r.Context(), audit.Filter{Query: q, Limit: auditPageSize, Offset: pager.Offset()})
	if err != nil {
		failErr(w, r, err)
		return
	}
	pager.Total = total
	renderPage(w, "audit", AuditPageData{
		Base:    s.base(w, r, "Audit log", "audit", Crumb{Label: "Security"}, Crumb{Label: "Audit log"}),
		Entries: views(entries),
		Pager:   pager,
		Query:   q,
	})
}
