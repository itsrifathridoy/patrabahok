package webui

import (
	"fmt"
	"net/http"
)

type SettingsPageData struct {
	Base
	Cloudflare CloudflareSectionData
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	cf := s.cloudflareSectionData(r)
	if r.URL.Query().Get("cfconnected") == "1" {
		cf.Notice = "Connected to Cloudflare."
	} else if errCode := r.URL.Query().Get("cferror"); errCode != "" {
		cf.Error = cloudflareErrorMessage(errCode)
	}
	data := SettingsPageData{
		Base:       Base{Title: "Settings", Active: "settings", Username: userFromContext(r).Username},
		Cloudflare: cf,
	}
	renderPage(w, "settings", data)
}

func cloudflareErrorMessage(code string) string {
	switch code {
	case "noclient":
		return "No Cloudflare OAuth client is configured yet — save your Client ID and Client Secret first."
	case "state_mismatch":
		return "The Cloudflare authorization response didn't match — please try connecting again."
	case "no_code":
		return "Cloudflare didn't return an authorization code — please try connecting again."
	case "exchange_failed":
		return "Could not complete the Cloudflare authorization — check the Client ID/Secret and redirect URL, then try again."
	case "save_failed":
		return "Authorized with Cloudflare, but saving the tokens failed — try connecting again."
	case "access_denied":
		return "Cloudflare authorization was denied or cancelled."
	default:
		return "Cloudflare authorization failed (" + code + ")."
	}
}

func (s *Server) handleSettingsPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	username := userFromContext(r).Username
	current := r.FormValue("current_password")
	next := r.FormValue("new_password")

	if len(next) < 8 {
		writeSettingsResult(w, false, "New password must be at least 8 characters.")
		return
	}
	if _, err := s.admins.Authenticate(r.Context(), username, current); err != nil {
		writeSettingsResult(w, false, "Current password is incorrect.")
		return
	}
	if err := s.admins.ChangePassword(r.Context(), username, next); err != nil {
		writeSettingsResult(w, false, "Could not update password. Try again.")
		return
	}
	writeSettingsResult(w, true, "Password updated.")
}

func writeSettingsResult(w http.ResponseWriter, ok bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	kind := "error"
	if ok {
		kind = "success"
	}
	fmt.Fprintf(w, `<div class="flash flash-%s">%s</div>`, kind, message)
}
