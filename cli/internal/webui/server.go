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
	"github.com/itsrifathridoy/patrabahok/cli/internal/audit"
	"github.com/itsrifathridoy/patrabahok/cli/internal/authtoken"
	"github.com/itsrifathridoy/patrabahok/cli/internal/cloudflare"
	"github.com/itsrifathridoy/patrabahok/cli/internal/domainkey"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/web"
)

type Server struct {
	store      *mailbox.Store
	admins     *adminauth.Store
	tokens     *authtoken.Store
	cloudflare *cloudflare.Store
	domainKeys *domainkey.Store
	audit      *audit.Store
	mux        *http.ServeMux
}

func New(db *sql.DB) *Server {
	s := &Server{
		store:      mailbox.NewStore(db),
		admins:     adminauth.NewStore(db),
		tokens:     authtoken.NewStore(db),
		cloudflare: cloudflare.NewStore(db),
		domainKeys: domainkey.NewStore(db),
		audit:      audit.NewStore(db),
		mux:        http.NewServeMux(),
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

	s.mux.HandleFunc("GET /{$}", s.requireAuth(s.handleOverviewPage))
	s.mux.HandleFunc("GET /search", s.requireAuth(s.handleSearch))

	s.mux.HandleFunc("GET /domains", s.requireAuth(s.handleDomainsPage))
	s.mux.HandleFunc("POST /domains", s.requireAuth(s.handleDomainAdd))
	s.mux.HandleFunc("GET /domains/{name}", s.requireAuth(s.handleDomainPage))
	s.mux.HandleFunc("DELETE /domains/{name}", s.requireAuth(s.handleDomainDelete))
	s.mux.HandleFunc("POST /domains/{name}/catchall", s.requireAuth(s.handleDomainCatchAll))
	s.mux.HandleFunc("POST /domains/cloudflare-apply", s.requireAuth(s.handleDomainsCloudflareApply))
	s.mux.HandleFunc("POST /domains/{name}/apikey", s.requireAuth(s.handleDomainAPIKeyGenerate))
	s.mux.HandleFunc("DELETE /domains/{name}/apikey", s.requireAuth(s.handleDomainAPIKeyRevoke))

	s.mux.HandleFunc("GET /users", s.requireAuth(s.handleUsersPage))
	s.mux.HandleFunc("POST /users", s.requireAuth(s.handleUserCreate))
	s.mux.HandleFunc("GET /users/import", s.requireAuth(s.handleUsersImportPage))
	s.mux.HandleFunc("POST /users/import", s.requireAuth(s.handleUsersImport))
	s.mux.HandleFunc("GET /users/{email}", s.requireAuth(s.handleUserPage))
	s.mux.HandleFunc("DELETE /users/{email}", s.requireAuth(s.handleUserDelete))
	s.mux.HandleFunc("POST /users/{email}/profile", s.requireAuth(s.handleUserProfile))
	s.mux.HandleFunc("POST /users/{email}/password", s.requireAuth(s.handleUserPassword))
	s.mux.HandleFunc("POST /users/{email}/quota", s.requireAuth(s.handleUserQuota))
	s.mux.HandleFunc("POST /users/{email}/status", s.requireAuth(s.handleUserStatus))
	s.mux.HandleFunc("POST /users/{email}/forward", s.requireAuth(s.handleUserForward))
	s.mux.HandleFunc("DELETE /users/{email}/forward", s.requireAuth(s.handleUserForwardClear))
	s.mux.HandleFunc("POST /users/{email}/aliases", s.requireAuth(s.handleUserAliasAdd))
	s.mux.HandleFunc("DELETE /users/{email}/aliases/{alias}", s.requireAuth(s.handleUserAliasRemove))
	s.mux.HandleFunc("POST /users/{email}/groups", s.requireAuth(s.handleUserGroupAdd))
	s.mux.HandleFunc("DELETE /users/{email}/groups/{group}", s.requireAuth(s.handleUserGroupRemove))
	// Pre-redesign URL.
	s.mux.HandleFunc("GET /mailboxes", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/users", http.StatusMovedPermanently)
	})

	s.mux.HandleFunc("GET /groups", s.requireAuth(s.handleGroupsPage))
	s.mux.HandleFunc("POST /groups", s.requireAuth(s.handleGroupCreate))
	s.mux.HandleFunc("GET /groups/{email}", s.requireAuth(s.handleGroupPage))
	s.mux.HandleFunc("POST /groups/{email}", s.requireAuth(s.handleGroupUpdate))
	s.mux.HandleFunc("DELETE /groups/{email}", s.requireAuth(s.handleGroupDelete))
	s.mux.HandleFunc("POST /groups/{email}/members", s.requireAuth(s.handleGroupMembersAdd))
	s.mux.HandleFunc("POST /groups/{email}/members/{member}/role", s.requireAuth(s.handleGroupMemberRole))
	s.mux.HandleFunc("DELETE /groups/{email}/members/{member}", s.requireAuth(s.handleGroupMemberRemove))

	s.mux.HandleFunc("GET /audit", s.requireAuth(s.handleAuditPage))

	s.mux.HandleFunc("GET /aliases", s.requireAuth(s.handleAliasesPage))
	s.mux.HandleFunc("POST /aliases", s.requireAuth(s.handleAliasAdd))
	s.mux.HandleFunc("DELETE /aliases", s.requireAuth(s.handleAliasDelete))

	s.mux.HandleFunc("GET /dkim", s.requireAuth(s.handleDKIMPage))
	s.mux.HandleFunc("GET /dkim/verify", s.requireAuth(s.handleDKIMVerify))
	s.mux.HandleFunc("POST /dkim/cloudflare-apply", s.requireAuth(s.handleDKIMCloudflareApply))
	s.mux.HandleFunc("POST /dkim/mta-sts-enable", s.requireAuth(s.handleDKIMMTASTSEnable))
	s.mux.HandleFunc("POST /dkim/rotate", s.requireAuth(s.handleDKIMRotate))

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
	s.mux.HandleFunc("POST /settings/cloudflare", s.requireAuth(s.handleCloudflareConnect))
	s.mux.HandleFunc("DELETE /settings/cloudflare", s.requireAuth(s.handleCloudflareDisconnect))
	s.mux.HandleFunc("POST /settings/cloudflare/oauth-client", s.requireAuth(s.handleCloudflareOAuthClientSave))
	s.mux.HandleFunc("GET /settings/cloudflare/authorize", s.requireAuth(s.handleCloudflareAuthorize))
	s.mux.HandleFunc("GET /settings/cloudflare/callback", s.requireAuth(s.handleCloudflareCallback))
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

// CertSource returns a tls.Config.GetCertificate function for the certificate at
// certPath/keyPath, reloaded automatically on renewal — the same one the dashboard
// uses, shared with patrabahokd's public HTTPS listener (internal/frontdoor).
func CertSource(certPath, keyPath string) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return newCertReloader(certPath, keyPath).GetCertificate
}

// ServeTLS starts the HTTPS listener for the dashboard on addr, using the
// certificate at certPath/keyPath (reloaded automatically on renewal).
func (s *Server) ServeTLS(addr, certPath, keyPath string) error {
	reloader := newCertReloader(certPath, keyPath)
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.secure(s.mux),
		TLSConfig:    &tls.Config{GetCertificate: reloader.GetCertificate, MinVersion: tls.VersionTLS12},
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}
	log.Printf("webui: listening on https://%s", addr)
	return srv.ListenAndServeTLS("", "")
}
