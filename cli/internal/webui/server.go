// Package webui implements the admin dashboard: server-rendered HTML (html/template,
// context-aware auto-escaping) with htmx for partial updates and Alpine.js for small
// bits of client-side interactivity, no build toolchain, no external CDN dependency —
// both libraries are vendored and embedded into the binary. Session-cookie
// authentication, separate from the JSON API's bearer tokens (see internal/api).
package webui

import (
	"crypto/tls"
	"database/sql"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/adminauth"
	"github.com/itsrifathridoy/patrabahok/cli/internal/authtoken"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/web"
)

type Server struct {
	store  *mailbox.Store
	admins *adminauth.Store
	tokens *authtoken.Store
	mux    *http.ServeMux
}

func New(db *sql.DB) *Server {
	s := &Server{
		store:  mailbox.NewStore(db),
		admins: adminauth.NewStore(db),
		tokens: authtoken.NewStore(db),
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	staticSub, err := fs.Sub(web.StaticFS, "static")
	if err == nil {
		s.mux.Handle("GET /static/", http.StripPrefix("/static/", cacheHeaders(http.FileServer(http.FS(staticSub)))))
	}

	s.mux.HandleFunc("GET /login", s.handleLoginForm)
	s.mux.HandleFunc("POST /login", s.handleLoginSubmit)
	s.mux.HandleFunc("POST /logout", s.handleLogout)

	s.mux.HandleFunc("GET /", s.requireAuth(s.handleOverviewPage))

	s.mux.HandleFunc("GET /domains", s.requireAuth(s.handleDomainsPage))
	s.mux.HandleFunc("POST /domains", s.requireAuth(s.handleDomainAdd))
	s.mux.HandleFunc("DELETE /domains/{name}", s.requireAuth(s.handleDomainDelete))

	s.mux.HandleFunc("GET /mailboxes", s.requireAuth(s.handleMailboxesPage))
	s.mux.HandleFunc("POST /mailboxes", s.requireAuth(s.handleMailboxAdd))
	s.mux.HandleFunc("DELETE /mailboxes/{email}", s.requireAuth(s.handleMailboxDelete))
	s.mux.HandleFunc("PUT /mailboxes/{email}/password", s.requireAuth(s.handleMailboxPassword))

	s.mux.HandleFunc("GET /aliases", s.requireAuth(s.handleAliasesPage))
	s.mux.HandleFunc("POST /aliases", s.requireAuth(s.handleAliasAdd))
	s.mux.HandleFunc("DELETE /aliases", s.requireAuth(s.handleAliasDelete))

	s.mux.HandleFunc("GET /dkim", s.requireAuth(s.handleDKIMPage))
	s.mux.HandleFunc("GET /dkim/verify", s.requireAuth(s.handleDKIMVerify))

	s.mux.HandleFunc("GET /queue", s.requireAuth(s.handleQueuePage))
	s.mux.HandleFunc("GET /queue/partial", s.requireAuth(s.handleQueuePartial))
	s.mux.HandleFunc("POST /queue/flush", s.requireAuth(s.handleQueueFlush))

	s.mux.HandleFunc("GET /diagnostics", s.requireAuth(s.handleDiagnosticsPage))
	s.mux.HandleFunc("GET /diagnostics/partial", s.requireAuth(s.handleDiagnosticsPartial))

	s.mux.HandleFunc("GET /tokens", s.requireAuth(s.handleTokensPage))
	s.mux.HandleFunc("POST /tokens", s.requireAuth(s.handleTokenAdd))
	s.mux.HandleFunc("DELETE /tokens/{name}", s.requireAuth(s.handleTokenDelete))

	s.mux.HandleFunc("GET /admins", s.requireAuth(s.handleAdminsPage))
	s.mux.HandleFunc("POST /admins", s.requireAuth(s.handleAdminAdd))
	s.mux.HandleFunc("DELETE /admins/{username}", s.requireAuth(s.handleAdminDelete))

	s.mux.HandleFunc("GET /settings", s.requireAuth(s.handleSettingsPage))
	s.mux.HandleFunc("POST /settings/password", s.requireAuth(s.handleSettingsPassword))
}

func cacheHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

// --- TLS certificate hot-reload -------------------------------------------------
//
// Reads cert/key from disk on each new TLS handshake, but only re-parses them when
// the files' mtimes change, so a certbot renewal takes effect without restarting
// patrabahokd.

type certReloader struct {
	certPath, keyPath string
	mu                sync.Mutex
	cert              *tls.Certificate
	certModTime       time.Time
}

func newCertReloader(certPath, keyPath string) *certReloader {
	return &certReloader{certPath: certPath, keyPath: keyPath}
}

func (c *certReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := os.Stat(c.certPath)
	if err != nil {
		if c.cert != nil {
			return c.cert, nil
		}
		return nil, err
	}
	if c.cert == nil || info.ModTime().After(c.certModTime) {
		cert, err := tls.LoadX509KeyPair(c.certPath, c.keyPath)
		if err != nil {
			if c.cert != nil {
				return c.cert, nil
			}
			return nil, err
		}
		c.cert = &cert
		c.certModTime = info.ModTime()
	}
	return c.cert, nil
}

// ServeTLS starts the HTTPS listener for the dashboard on addr, using the
// certificate at certPath/keyPath (reloaded automatically on renewal).
func (s *Server) ServeTLS(addr, certPath, keyPath string) error {
	reloader := newCertReloader(certPath, keyPath)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		TLSConfig:    &tls.Config{GetCertificate: reloader.GetCertificate, MinVersion: tls.VersionTLS12},
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	log.Printf("webui: listening on https://%s", addr)
	return srv.ListenAndServeTLS("", "")
}
