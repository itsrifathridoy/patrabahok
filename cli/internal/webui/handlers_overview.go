package webui

import (
	"net/http"
	"sort"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/diag"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

type DomainBar struct {
	Name    string
	Users   int
	Percent int
}

type OverviewPageData struct {
	Base
	DomainCount    int
	UserCount      int
	SuspendedCount int
	GroupCount     int
	AliasCount     int
	StorageUsed    int64
	StorageQuota   int64
	DomainBars     []DomainBar
	QueueEmpty     bool
	QueueKnown     bool
	Services       []diag.ServiceStatus
	ServicesUp     int
	ServicesTotal  int
	DiskKnown      bool
	DiskUsed       int64
	DiskTotal      int64
	DiskPercent    int
	MailHost       string
	Activity       []AuditView
}

// maxStorageScan bounds how many mailboxes the overview reads usage files for, so the
// page stays fast on very large installs. Above it, storage shows as "not measured".
const maxStorageScan = 5000

func (s *Server) handleOverviewPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := OverviewPageData{Base: s.base(w, r, "Overview", "overview")}

	if stats, err := s.store.DomainStatsAll(ctx); err == nil {
		data.DomainCount = len(stats)
		max := 0
		for name, st := range stats {
			data.UserCount += st.Users
			data.SuspendedCount += st.Suspended
			data.GroupCount += st.Groups
			data.AliasCount += st.Aliases
			data.DomainBars = append(data.DomainBars, DomainBar{Name: name, Users: st.Users})
			if st.Users > max {
				max = st.Users
			}
		}
		sort.Slice(data.DomainBars, func(i, j int) bool {
			if data.DomainBars[i].Users != data.DomainBars[j].Users {
				return data.DomainBars[i].Users > data.DomainBars[j].Users
			}
			return data.DomainBars[i].Name < data.DomainBars[j].Name
		})
		if len(data.DomainBars) > 6 {
			data.DomainBars = data.DomainBars[:6]
		}
		for i := range data.DomainBars {
			data.DomainBars[i].Percent = percent(int64(data.DomainBars[i].Users), int64(max))
		}
	}

	if data.UserCount <= maxStorageScan {
		if list, _, err := s.store.MailboxSearch(ctx, mailbox.MailboxFilter{Limit: maxStorageScan}); err == nil {
			for _, m := range list {
				data.StorageUsed += mailbox.UsageBytes(m.Email)
				data.StorageQuota += m.QuotaBytes
			}
		}
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
		data.DiskKnown = true
		data.DiskTotal = int64(disk.TotalBytes)
		data.DiskUsed = int64(disk.TotalBytes - disk.FreeBytes)
		data.DiskPercent = int(disk.UsedPercent + 0.5)
	}
	data.MailHost, _, _ = stateConfigStrings()
	data.Activity = s.recentActivity(r, "", 8)

	renderPage(w, "overview", data)
}

type SearchPageData struct {
	Base
	Query   string
	Users   []mailbox.Mailbox
	Groups  []mailbox.Group
	Domains []mailbox.Domain
	Total   int
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data := SearchPageData{Base: s.base(w, r, "Search", "", Crumb{Label: "Search"}), Query: q}
	if q != "" {
		var err error
		if data.Users, _, err = s.store.MailboxSearch(ctx, mailbox.MailboxFilter{Query: q, Limit: 25}); err != nil {
			failErr(w, r, err)
			return
		}
		if data.Groups, err = s.store.GroupList(ctx, "", q); err != nil {
			failErr(w, r, err)
			return
		}
		domains, err := s.store.DomainList(ctx)
		if err != nil {
			failErr(w, r, err)
			return
		}
		for _, d := range domains {
			if strings.Contains(strings.ToLower(d.Name), strings.ToLower(q)) {
				data.Domains = append(data.Domains, d)
			}
		}
		data.Total = len(data.Users) + len(data.Groups) + len(data.Domains)
		// An exact address match goes straight to it.
		if data.Total == 1 {
			switch {
			case len(data.Users) == 1 && strings.EqualFold(data.Users[0].Email, q):
				http.Redirect(w, r, userURL(data.Users[0].Email), http.StatusSeeOther)
				return
			case len(data.Groups) == 1 && strings.EqualFold(data.Groups[0].Email, q):
				http.Redirect(w, r, groupURL(data.Groups[0].Email), http.StatusSeeOther)
				return
			}
		}
	}
	renderPage(w, "search", data)
}
