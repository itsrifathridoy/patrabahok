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

type DomainsPageData struct {
	Base
	Domains         []mailbox.Domain
	JustAdded       string
	JustAddedCFZone string
	Cloudflare      CloudflareSectionData
}

func (s *Server) domainsData(r *http.Request) (DomainsPageData, error) {
	domains, err := s.store.DomainList(r.Context())
	if err != nil {
		return DomainsPageData{}, err
	}
	return DomainsPageData{
		Base:       Base{Title: "Domains", Active: "domains", Username: userFromContext(r).Username},
		Domains:    domains,
		Cloudflare: s.cloudflareSectionData(r),
	}, nil
}

func (s *Server) handleDomainsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.domainsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("cfconnected") == "1" {
		data.Cloudflare.Notice = "Connected to Cloudflare."
	} else if errCode := r.URL.Query().Get("cferror"); errCode != "" {
		data.Cloudflare.Error = cloudflareErrorMessage(errCode)
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
	if token, terr := s.cloudflare.Token(r.Context()); terr == nil && token != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		_, zoneName, found, ferr := cloudflare.New(token).FindZone(ctx, name)
		cancel()
		if ferr == nil && found {
			data.JustAddedCFZone = zoneName
		}
	}
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

// handleDomainsCloudflareApply is the one-click "Auto-configure via Cloudflare" action
// offered right in the post-add banner (see domains.html) — same underlying logic as
// the DNS Analysis page's version, just rendering a small result fragment inline
// instead of a full page swap.
func (s *Server) handleDomainsCloudflareApply(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		http.Error(w, "domain is required", http.StatusBadRequest)
		return
	}

	token, err := s.cloudflare.Token(r.Context())
	if err != nil || token == "" {
		renderPartial(w, "domains", "cf_quick_apply_result", cfQuickApplyData{Err: "Cloudflare isn't connected."})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	cf := cloudflare.New(token)
	zoneID, _, found, ferr := cf.FindZone(ctx, domain)
	if ferr != nil || !found {
		msg := "No matching Cloudflare zone was found for " + domain + "."
		if ferr != nil {
			msg = "Could not look up the Cloudflare zone: " + ferr.Error()
		}
		renderPartial(w, "domains", "cf_quick_apply_result", cfQuickApplyData{Err: msg})
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
	renderPartial(w, "domains", "cf_quick_apply_result", cfQuickApplyData{Domain: domain, Results: results})
}

type cfQuickApplyData struct {
	Domain  string
	Results []cloudflare.ApplyResult
	Err     string
}
