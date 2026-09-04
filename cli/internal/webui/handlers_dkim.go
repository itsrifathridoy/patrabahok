package webui

import (
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/dnscheck"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

type DKIMPageData struct {
	Base
	Domains    []mailbox.Domain
	Selected   string
	RawRecords string
	Report     *dnscheck.Report
	Checked    bool
}

func stateConfigStrings() (mailHost, serverIP string) {
	st, err := sysinfo.InstallerState()
	if err != nil {
		return "", ""
	}
	cfg, _ := st["config"].(map[string]any)
	if cfg == nil {
		return "", ""
	}
	if v, ok := cfg["hostname"].(string); ok {
		mailHost = v
	}
	if v, ok := cfg["server_ip"].(string); ok {
		serverIP = v
	}
	return
}

func (s *Server) dkimData(r *http.Request, runCheck bool) (DKIMPageData, error) {
	domains, err := s.store.DomainList(r.Context())
	if err != nil {
		return DKIMPageData{}, err
	}
	selected := r.URL.Query().Get("domain")
	if selected == "" && len(domains) > 0 {
		selected = domains[0].Name
	}

	data := DKIMPageData{
		Base:     Base{Title: "DNS Analysis", Active: "dkim", Username: userFromContext(r).Username},
		Domains:  domains,
		Selected: selected,
	}
	if selected == "" {
		return data, nil
	}

	data.RawRecords, _ = sysinfo.DNSRecords(selected)

	if runCheck {
		mailHost, serverIP := stateConfigStrings()
		dkimText, _ := sysinfo.DKIMRecord(selected)
		report := dnscheck.Analyze(selected, mailHost, serverIP, dkimText)
		data.Report = &report
		data.Checked = true
	}
	return data, nil
}

func (s *Server) handleDKIMPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.dkimData(r, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "dkim", data)
}

func (s *Server) handleDKIMVerify(w http.ResponseWriter, r *http.Request) {
	data, err := s.dkimData(r, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPartial(w, "dkim", "dns_analysis", data)
}
