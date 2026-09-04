package webui

import (
	"context"
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/adminauth"
)

type ctxKey int

const userCtxKey ctxKey = iota

func userFromContext(r *http.Request) *adminauth.AdminUser {
	u, _ := r.Context().Value(userCtxKey).(*adminauth.AdminUser)
	return u
}

func (s *Server) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(adminauth.SessionCookieName)
		if err != nil {
			redirectToLogin(w, r)
			return
		}
		user, err := s.admins.VerifySession(r.Context(), cookie.Value)
		if err != nil {
			clearSessionCookie(w)
			redirectToLogin(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		h(w, r.WithContext(ctx))
	}
}

func redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// SameSite=Lax, not Strict: the Cloudflare "Connect" OAuth flow (see
// handlers_cloudflare.go) lands the browser back on this app via a cross-site top-level
// redirect from dash.cloudflare.com, which Strict cookies are never sent on — that would
// break the callback's requireAuth check entirely. Lax still blocks the cookie on
// cross-site POST/fetch requests (the actual state-changing actions), which is what
// matters for CSRF here, so this keeps the same practical protection while allowing the
// one thing Strict can't do: an external redirect back to a logged-in page.
const sessionCookieSameSite = http.SameSiteLaxMode

func setSessionCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminauth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sessionCookieSameSite,
		MaxAge:   7 * 24 * 60 * 60,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminauth.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: sessionCookieSameSite,
		MaxAge:   -1,
	})
}
