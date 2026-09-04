package webui

import (
	"context"
	"net/http"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/cloudflare"
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

	CloudflareConnected    bool
	CloudflareZone         string
	CloudflareApplyResults []cloudflare.ApplyResult
	CloudflareApplyErr     string
}

func stateConfigStrings() (mailHost, serverIP, adminEmail string) {
	st, err := sysinfo.InstallerState()
	if err != nil {
		return "", "", ""
	}
	cfg, _ := st["config"].(map[string]any)
	if cfg == nil {
		return "", "", ""
	}
	if v, ok := cfg["hostname"].(string); ok {
		mailHost = v
	}
	if v, ok := cfg["server_ip"].(string); ok {
		serverIP = v
	}
	if v, ok := cfg["admin_email"].(string); ok {
		adminEmail = v
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

	if token, err := s.cloudflare.Token(r.Context()); err == nil && token != "" {
		data.CloudflareConnected = true
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		_, zoneName, found, ferr := cloudflare.New(token).FindZone(ctx, selected)
		cancel()
		if ferr == nil && found {
			data.CloudflareZone = zoneName
		}
	}

	if runCheck {
		mailHost, serverIP, _ := stateConfigStrings()
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

// handleDKIMCloudflareApply creates/updates the required DNS records directly via the
// Cloudflare API for the selected domain's zone, then re-runs the live verify so the
// result reflects reality immediately rather than asking the admin to come back later.
func (s *Server) handleDKIMCloudflareApply(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}

	token, err := s.cloudflare.Token(r.Context())
	if err != nil || token == "" {
		data, _ := s.dkimData(r, true)
		data.CloudflareApplyErr = "Cloudflare isn't connected — add an API token in Settings first."
		renderPartial(w, "dkim", "dns_analysis", data)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	cf := cloudflare.New(token)
	zoneID, _, found, ferr := cf.FindZone(ctx, domain)
	if ferr != nil || !found {
		data, _ := s.dkimData(r, true)
		if ferr != nil {
			data.CloudflareApplyErr = "Could not look up the Cloudflare zone: " + ferr.Error()
		} else {
			data.CloudflareApplyErr = "No zone matching " + domain + " was found in your connected Cloudflare account."
		}
		renderPartial(w, "dkim", "dns_analysis", data)
		return
	}

	mailHost, serverIP, adminEmail := stateConfigStrings()
	if adminEmail == "" {
		adminEmail = "postmaster@" + domain
	}
	dkimText, _ := sysinfo.DKIMRecord(domain)
	dkimValue := dnscheck.FullDKIMRecordValue(dkimText)
	dmarcValue := "v=DMARC1; p=none; rua=mailto:" + adminEmail

	results := cloudflare.ApplyMailRecords(ctx, cf, zoneID, domain, mailHost, serverIP, dkimValue, dmarcValue)

	data, err := s.dkimData(r, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data.CloudflareApplyResults = results
	renderPartial(w, "dkim", "dns_analysis", data)
}
