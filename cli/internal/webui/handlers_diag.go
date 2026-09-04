package webui

import (
	"net/http"
	"path/filepath"

	"github.com/itsrifathridoy/patrabahok/cli/internal/diag"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

type DiagPageData struct {
	Base
	Services    []diag.ServiceStatus
	Configs     []diag.ConfigCheck
	TLS         diag.TLSInfo
	TLSKnown    bool
	Disk        diag.DiskUsage
	DiskKnown   bool
	MailIssues  []string
	Fail2ban    []diag.BannedIP
	Fail2banErr string
}

func (s *Server) diagData(r *http.Request) DiagPageData {
	data := DiagPageData{
		Base:     Base{Title: "Diagnostics", Active: "diagnostics", Username: userFromContext(r).Username},
		Services: diag.CheckServices(),
		Configs:  diag.CheckConfigs(),
	}

	if st, err := sysinfo.InstallerState(); err == nil {
		if cfg, ok := st["config"].(map[string]any); ok {
			if hostname, ok := cfg["hostname"].(string); ok && hostname != "" {
				certPath := filepath.Join("/etc/letsencrypt/live", hostname, "fullchain.pem")
				data.TLS = diag.CheckTLS(certPath)
				data.TLSKnown = data.TLS.Error == ""
			}
		}
	}

	if disk, err := diag.CheckDisk("/"); err == nil {
		data.Disk = disk
		data.DiskKnown = true
	}

	if issues, err := diag.RecentIssues("/var/log/mail.log", 40); err == nil {
		data.MailIssues = issues
	}

	if bans, err := diag.Fail2banBans(); err == nil {
		data.Fail2ban = bans
	} else {
		data.Fail2banErr = err.Error()
	}

	return data
}

func (s *Server) handleDiagnosticsPage(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "diagnostics", s.diagData(r))
}

func (s *Server) handleDiagnosticsPartial(w http.ResponseWriter, r *http.Request) {
	renderPartial(w, "diagnostics", "diagnostics_body", s.diagData(r))
}
