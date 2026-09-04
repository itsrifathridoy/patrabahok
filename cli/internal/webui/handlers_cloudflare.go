package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/cloudflare"
)

const oauthStateCookie = "patrabahok_cf_oauth_state"

type CloudflareSectionData struct {
	Settings    *cloudflare.Settings
	RedirectURI string
	Error       string
	Notice      string
}

func cloudflareRedirectURI(r *http.Request) string {
	mailHost, _, _ := stateConfigStrings()
	host := mailHost
	if host == "" {
		host = r.Host // best-effort fallback if installer state is unavailable
	} else {
		host = host + ":8443"
	}
	return "https://" + host + "/settings/cloudflare/callback"
}

func (s *Server) cloudflareSectionData(r *http.Request) CloudflareSectionData {
	settings, err := s.cloudflare.Get(r.Context())
	if err != nil {
		return CloudflareSectionData{Settings: &cloudflare.Settings{}, RedirectURI: cloudflareRedirectURI(r), Error: err.Error()}
	}
	return CloudflareSectionData{Settings: settings, RedirectURI: cloudflareRedirectURI(r)}
}

// handleCloudflareConnect connects via a manually pasted, scoped API token — the
// simpler fallback to the OAuth "Connect with Cloudflare" flow below.
func (s *Server) handleCloudflareConnect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	token := strings.TrimSpace(r.FormValue("token"))
	if token == "" {
		data := s.cloudflareSectionData(r)
		data.Error = "API token is required."
		renderPartial(w, "settings", "cloudflare_section", data)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := cloudflare.New(token).VerifyToken(ctx); err != nil {
		data := s.cloudflareSectionData(r)
		data.Error = "Could not verify this token with Cloudflare: " + err.Error()
		renderPartial(w, "settings", "cloudflare_section", data)
		return
	}

	if err := s.cloudflare.SetAPIToken(r.Context(), token); err != nil {
		data := s.cloudflareSectionData(r)
		data.Error = "Token verified, but saving it failed: " + err.Error()
		renderPartial(w, "settings", "cloudflare_section", data)
		return
	}

	renderPartial(w, "settings", "cloudflare_section", s.cloudflareSectionData(r))
}

// handleCloudflareOAuthClientSave stores the admin's own Cloudflare OAuth client
// (Manage Account > OAuth clients, redirect URI = cloudflareRedirectURI) — step one of
// the OAuth flow, before they've clicked through to actually authorize anything.
func (s *Server) handleCloudflareOAuthClientSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	clientID := strings.TrimSpace(r.FormValue("client_id"))
	clientSecret := strings.TrimSpace(r.FormValue("client_secret"))
	if clientID == "" || clientSecret == "" {
		data := s.cloudflareSectionData(r)
		data.Error = "Client ID and Client Secret are both required."
		renderPartial(w, "settings", "cloudflare_section", data)
		return
	}
	if err := s.cloudflare.SetOAuthClient(r.Context(), clientID, clientSecret); err != nil {
		data := s.cloudflareSectionData(r)
		data.Error = "Could not save the OAuth client: " + err.Error()
		renderPartial(w, "settings", "cloudflare_section", data)
		return
	}
	renderPartial(w, "settings", "cloudflare_section", s.cloudflareSectionData(r))
}

// handleCloudflareAuthorize sends the browser to Cloudflare's consent screen.
func (s *Server) handleCloudflareAuthorize(w http.ResponseWriter, r *http.Request) {
	oc, ok, err := s.cloudflare.OAuthClientFor(r.Context(), cloudflareRedirectURI(r))
	if err != nil || !ok {
		http.Redirect(w, r, "/settings?cferror=noclient", http.StatusSeeOther)
		return
	}

	stateRaw := make([]byte, 24)
	_, _ = rand.Read(stateRaw)
	state := hex.EncodeToString(stateRaw)

	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/settings/cloudflare",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode, // must survive the cross-site redirect back from Cloudflare
		MaxAge:   600,
	})
	http.Redirect(w, r, oc.BuildAuthorizeURL(state), http.StatusSeeOther)
}

// handleCloudflareCallback completes the OAuth flow: Cloudflare redirects here with
// ?code=&state= after the admin approves the consent screen.
func (s *Server) handleCloudflareCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(oauthStateCookie)
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: "", Path: "/settings/cloudflare", MaxAge: -1})

	q := r.URL.Query()
	if errMsg := q.Get("error"); errMsg != "" {
		http.Redirect(w, r, "/settings?cferror="+errMsg, http.StatusSeeOther)
		return
	}
	if err != nil || q.Get("state") == "" || q.Get("state") != stateCookie.Value {
		http.Redirect(w, r, "/settings?cferror=state_mismatch", http.StatusSeeOther)
		return
	}
	code := q.Get("code")
	if code == "" {
		http.Redirect(w, r, "/settings?cferror=no_code", http.StatusSeeOther)
		return
	}

	oc, ok, err := s.cloudflare.OAuthClientFor(r.Context(), cloudflareRedirectURI(r))
	if err != nil || !ok {
		http.Redirect(w, r, "/settings?cferror=noclient", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	result, err := oc.ExchangeCode(ctx, code)
	if err != nil {
		http.Redirect(w, r, "/settings?cferror=exchange_failed", http.StatusSeeOther)
		return
	}
	if err := s.cloudflare.SaveOAuthCallback(r.Context(), result); err != nil {
		http.Redirect(w, r, "/settings?cferror=save_failed", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/settings?cfconnected=1", http.StatusSeeOther)
}

func (s *Server) handleCloudflareDisconnect(w http.ResponseWriter, r *http.Request) {
	if err := s.cloudflare.Clear(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPartial(w, "settings", "cloudflare_section", s.cloudflareSectionData(r))
}
