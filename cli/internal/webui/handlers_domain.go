package webui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/cloudflare"
	"github.com/itsrifathridoy/patrabahok/cli/internal/dnscheck"
	"github.com/itsrifathridoy/patrabahok/cli/internal/domainkey"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

type DomainView struct {
	mailbox.Domain
	Stats  mailbox.DomainStats
	APIKey *domainkey.KeyInfo
}

type DomainsPageData struct {
	Base
	Domains    []DomainView
	Cloudflare CloudflareSectionData
}

func (s *Server) domainViews(r *http.Request) ([]DomainView, error) {
	domains, err := s.store.DomainList(r.Context())
	if err != nil {
		return nil, err
	}
	stats, err := s.store.DomainStatsAll(r.Context())
	if err != nil {
		return nil, err
	}
	keys, err := s.domainKeys.List(r.Context())
	if err != nil {
		return nil, err
	}
	byDomain := make(map[string]*domainkey.KeyInfo, len(keys))
	for i := range keys {
		byDomain[keys[i].Domain] = &keys[i]
	}
	views := make([]DomainView, 0, len(domains))
	for _, d := range domains {
		views = append(views, DomainView{Domain: d, Stats: stats[d.Name], APIKey: byDomain[d.Name]})
	}
	return views, nil
}

func (s *Server) handleDomainsPage(w http.ResponseWriter, r *http.Request) {
	views, err := s.domainViews(r)
	if err != nil {
		failErr(w, r, err)
		return
	}
	data := DomainsPageData{
		Base:       s.base(w, r, "Domains", "domains", Crumb{Label: "Domains"}),
		Domains:    views,
		Cloudflare: s.cloudflareSectionData(r),
	}
	if r.URL.Query().Get("cfconnected") == "1" {
		data.Cloudflare.Notice = "Connected to Cloudflare."
	} else if errCode := r.URL.Query().Get("cferror"); errCode != "" {
		data.Cloudflare.Error = cloudflareErrorMessage(errCode)
	}
	renderPage(w, "domains", data)
}

func domainURL(name string) string { return "/domains/" + url.PathEscape(name) }

func (s *Server) handleDomainAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, r, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	name := strings.ToLower(formValue(r, "name"))
	if err := s.store.DomainAdd(r.Context(), name); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "domain.add", name, "")
	done(w, r, domainURL(name), name+" added. Publish its DNS records next.")
}

type DomainPageData struct {
	Base
	Domain   DomainView
	MailHost string
	CatchAll string
	CFZone   string
	// NewAPIKey is set only in the response that generated it.
	NewAPIKey string
	Activity  []AuditView
}

func (s *Server) domainPageData(w http.ResponseWriter, r *http.Request, name string) (*DomainPageData, error) {
	views, err := s.domainViews(r)
	if err != nil {
		return nil, err
	}
	for _, v := range views {
		if v.Name != name {
			continue
		}
		data := &DomainPageData{
			Base:   s.base(w, r, v.Name, "domains", Crumb{Label: "Domains", Href: "/domains"}, Crumb{Label: v.Name}),
			Domain: v,
		}
		data.MailHost, _, _ = stateConfigStrings()
		if data.CatchAll, err = s.store.CatchAllGet(r.Context(), name); err != nil {
			return nil, err
		}
		return data, nil
	}
	return nil, nil
}

func (s *Server) handleDomainPage(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(r.PathValue("name"))
	data, err := s.domainPageData(w, r, name)
	if err != nil {
		failErr(w, r, err)
		return
	}
	if data == nil {
		http.NotFound(w, r)
		return
	}
	if token, terr := s.cloudflare.Token(r.Context()); terr == nil && token != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		_, zoneName, found, ferr := cloudflare.New(token).FindZone(ctx, name)
		cancel()
		if ferr == nil && found {
			data.CFZone = zoneName
		}
	}
	data.Activity = s.recentActivity(r, name, 8)
	renderPage(w, "domain", data)
}

func (s *Server) handleDomainDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if formValue(r, "confirm") != name {
		fail(w, r, http.StatusBadRequest, "Type the domain name exactly to confirm deletion.")
		return
	}
	if err := s.store.DomainRemove(r.Context(), name); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "domain.delete", name, "")
	done(w, r, "/domains", name+" and all of its users, groups and aliases were deleted.")
}

func (s *Server) handleDomainCatchAll(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	dest := formValue(r, "destination")
	if r.FormValue("enabled") != "1" {
		dest = ""
	}
	if err := s.store.CatchAllSet(r.Context(), name, dest); err != nil {
		failErr(w, r, err)
		return
	}
	if dest == "" {
		s.record(r, "domain.catchall.off", name, "")
		done(w, r, domainURL(name), "Catch-all turned off. Mail to unknown addresses is rejected.")
		return
	}
	s.record(r, "domain.catchall.set", name, dest)
	done(w, r, domainURL(name), "Mail to unknown addresses at "+name+" now goes to "+strings.ToLower(dest)+".")
}

func (s *Server) respondAPIKeyCard(w http.ResponseWriter, r *http.Request, name, newKey string) {
	data, err := s.domainPageData(w, r, name)
	if err != nil {
		failErr(w, r, err)
		return
	}
	if data == nil {
		fail(w, r, http.StatusNotFound, "No such domain.")
		return
	}
	data.NewAPIKey = newKey
	renderPartial(w, "domain", "apikey_card", data)
}

func (s *Server) handleDomainAPIKeyGenerate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	plaintext, err := s.domainKeys.Generate(r.Context(), name)
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.record(r, "apikey.generate", name, "")
	s.respondAPIKeyCard(w, r, name, plaintext)
}

func (s *Server) handleDomainAPIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.domainKeys.Revoke(r.Context(), name); err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.record(r, "apikey.revoke", name, "")
	s.respondAPIKeyCard(w, r, name, "")
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
