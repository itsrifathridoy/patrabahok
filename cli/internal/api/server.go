// Package api implements patrabahokd's local JSON API: a Unix-socket (by default)
// HTTP server, bearer-token authenticated, exposing the same operations as the CLI
// for automation/integration use.
package api

import (
	"context"
	"database/sql"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/authtoken"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

type Server struct {
	store  *mailbox.Store
	tokens *authtoken.Store
	mux    *http.ServeMux
}

func New(db *sql.DB) *Server {
	s := &Server{
		store:  mailbox.NewStore(db),
		tokens: authtoken.NewStore(db),
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

// ListenUnix listens on a Unix domain socket, replacing any stale socket file left
// behind by a previous run, and sets the given permission mode.
func ListenUnix(path string, mode os.FileMode) (net.Listener, error) {
	_ = os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, mode); err != nil {
		l.Close()
		return nil, err
	}
	return l, nil
}

func (s *Server) Serve(l net.Listener) error {
	srv := &http.Server{
		Handler:      s.loggingMiddleware(s.mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	return srv.Serve(l)
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

type ctxKey int

const tokenCtxKey ctxKey = iota

// requireScope wraps a handler with bearer-token authentication and a scope check.
func (s *Server) requireScope(scope string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		plaintext := strings.TrimSpace(strings.TrimPrefix(auth, prefix))
		tok, err := s.tokens.Verify(r.Context(), plaintext)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "invalid or revoked token")
			return
		}
		if !tok.HasScope(scope) {
			writeError(w, http.StatusForbidden, "token lacks required scope: "+scope)
			return
		}
		ctx := context.WithValue(r.Context(), tokenCtxKey, tok)
		h(w, r.WithContext(ctx))
	}
}
