package webui

import (
	"fmt"
	"net/http"
)

func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	renderPage(w, "settings", Base{Title: "Settings", Active: "settings", Username: userFromContext(r).Username})
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
