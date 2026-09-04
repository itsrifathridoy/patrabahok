package webui

import (
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/adminauth"
)

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminauth.SessionCookieName); err == nil {
		if _, err := s.admins.VerifySession(r.Context(), cookie.Value); err == nil {
			http.Redirect(w, r, "/domains", http.StatusSeeOther)
			return
		}
	}
	renderLogin(w, http.StatusOK, "")
}

func (s *Server) handleLoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		renderLogin(w, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	user, err := s.admins.Authenticate(r.Context(), username, password)
	if err != nil {
		logFailedLogin(username, r.RemoteAddr)
		renderLogin(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	token, _, err := s.admins.CreateSession(r.Context(), user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		renderLogin(w, http.StatusInternalServerError, "Could not start a session. Try again.")
		return
	}
	setSessionCookie(w, token, r.TLS != nil)
	http.Redirect(w, r, "/domains", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(adminauth.SessionCookieName); err == nil {
		_ = s.admins.RevokeSession(r.Context(), cookie.Value)
	}
	clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
