package webui

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// secure wraps every dashboard response with protective headers and rejects
// cross-origin state-changing requests. The session cookie is SameSite=Lax, which
// already keeps it off cross-site POSTs; checking Origin as well covers browsers and
// request types where that guarantee is weaker, and costs nothing for real use.
func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Strict-Transport-Security", "max-age=31536000")
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || !strings.EqualFold(u.Host, r.Host) {
					http.Error(w, "cross-origin request rejected", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isSentinel(err, target error) bool { return errors.Is(err, target) }

// record writes an audit entry attributed to the signed-in admin.
func (s *Server) record(r *http.Request, action, target, detail string) {
	actor := "admin"
	if u := userFromContext(r); u != nil {
		actor = "admin:" + u.Username
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	s.audit.Record(r.Context(), actor, action, target, detail, host)
}

// formValue returns a trimmed form field.
func formValue(r *http.Request, name string) string {
	return strings.TrimSpace(r.FormValue(name))
}
