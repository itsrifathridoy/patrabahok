package webui

import (
	"net/http"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/diag"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

type OverviewPageData struct {
	Base
	DomainCount   int
	MailboxCount  int
	AliasCount    int
	TokenCount    int
	QueueEmpty    bool
	QueueKnown    bool
	Services      []diag.ServiceStatus
	ServicesUp    int
	ServicesTotal int
	Disk          diag.DiskUsage
	DiskKnown     bool
	MailHost      string
	InstallerOK   bool
}

func (s *Server) handleOverviewPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := OverviewPageData{
		Base: Base{Title: "Overview", Active: "overview", Username: userFromContext(r).Username},
	}

	if domains, err := s.store.DomainList(ctx); err == nil {
		data.DomainCount = len(domains)
	}
	if mailboxes, err := s.store.MailboxList(ctx, ""); err == nil {
		data.MailboxCount = len(mailboxes)
	}
	if aliases, err := s.store.AliasList(ctx, ""); err == nil {
		data.AliasCount = len(aliases)
	}
	if tokens, err := s.tokens.List(ctx); err == nil {
		data.TokenCount = len(tokens)
	}

	if q, err := sysinfo.QueueList(); err == nil {
		data.QueueKnown = true
		data.QueueEmpty = strings.Contains(strings.ToLower(q), "mail queue is empty")
	}

	data.Services = diag.CheckServices()
	data.ServicesTotal = len(data.Services)
	for _, sv := range data.Services {
		if sv.Active {
			data.ServicesUp++
		}
	}

	if disk, err := diag.CheckDisk("/"); err == nil {
		data.Disk = disk
		data.DiskKnown = true
	}

	if st, err := sysinfo.InstallerState(); err == nil {
		data.InstallerOK = true
		if cfg, ok := st["config"].(map[string]any); ok {
			if v, ok := cfg["hostname"].(string); ok {
				data.MailHost = v
			}
		}
	}

	renderPage(w, "overview", data)
}
