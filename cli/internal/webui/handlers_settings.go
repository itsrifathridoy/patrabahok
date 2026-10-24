package webui

import (
	"net/http"
)

type SettingsPageData struct {
	Base
	MailHost string
}

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	data := SettingsPageData{Base: s.base(w, r, "Settings", "settings", Crumb{Label: "Settings"})}
	data.MailHost, _, _ = stateConfigStrings()
	renderPage(w, "settings", data)
}

func (s *Server) handleSettingsPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, r, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	username := userFromContext(r).Username
	current := r.FormValue("current_password")
	next := r.FormValue("new_password")

	if len(next) < 8 {
		fail(w, r, http.StatusBadRequest, "The new password must be at least 8 characters.")
		return
	}
	if next != r.FormValue("confirm_password") {
		fail(w, r, http.StatusBadRequest, "The new passwords don't match.")
		return
	}
	if _, err := s.admins.Authenticate(r.Context(), username, current); err != nil {
		fail(w, r, http.StatusBadRequest, "Your current password is incorrect.")
		return
	}
	if err := s.admins.ChangePassword(r.Context(), username, next); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "admin.password.change", username, "")
	done(w, r, "/settings", "Your password was changed.")
}
