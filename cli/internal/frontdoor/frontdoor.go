// Package frontdoor is patrabahokd's public HTTPS listener on :443 — the one port the
// outside world reaches over the web. It terminates TLS itself and routes by host and path:
//
//   - Host mta-sts.<domain>          → MTA-STS policy files (internal/mtasts)
//   - /api/<domain>/...              → the per-domain public API (internal/publicapi)
//   - everything else                → the webmail client chosen at install time
//     (Roundcube behind a loopback nginx/PHP-FPM, or patrabahok Mail), reverse-proxied
//
// The public API's first path segment is always a domain name, so it always contains a
// dot. patrabahok Mail has its own /api/accounts, /api/auth, ... routes with no dot, and
// those still go to the webmail. That rule is what lets both share the /api/ prefix the
// public API's URLs were specified with.
//
// Certificates are picked per connection by SNI: mta-sts.* names get their per-domain
// certificate, and every other name gets the mail hostname's certificate. All of them
// reload on renewal without a restart.
package frontdoor

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mtasts"
)

type Config struct {
	Addr string
	// HostCert is the GetCertificate source for the mail hostname. When nil, only
	// MTA-STS names can complete a TLS handshake. That is the pre-public-API behavior,
	// kept for installs started without -web-cert.
	HostCert func(*tls.ClientHelloInfo) (*tls.Certificate, error)
	// API serves /api/<domain>/...; nil disables it.
	API http.Handler
	// WebmailUpstream is the loopback http:// URL of the webmail backend; "" disables it.
	WebmailUpstream string
}

func ListenAndServeTLS(cfg Config) error {
	mtastsCert := mtasts.CertSource()
	getCert := func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if mtasts.IsPolicyHost(hello.ServerName) {
			return mtastsCert(hello)
		}
		if cfg.HostCert != nil {
			return cfg.HostCert(hello)
		}
		return nil, fmt.Errorf("frontdoor: no certificate for %q", hello.ServerName)
	}

	h, err := handler(cfg)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h,
		TLSConfig:         &tls.Config{GetCertificate: getCert, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
		// Generous enough for a webmail attachment upload/download over a slow link; the
		// API bounds its own request bodies separately (internal/publicapi).
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  2 * time.Minute,
		// Silences per-connection handshake-failure logging: :443 draws constant
		// unsolicited TLS probing from internet-wide scanners, and each one would
		// otherwise produce a log line for a rejection that isn't an operational problem.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	log.Printf("frontdoor: listening on https://%s (mta-sts: on, public API: %t, webmail: %s)",
		cfg.Addr, cfg.API != nil, orNone(cfg.WebmailUpstream))
	return srv.ListenAndServeTLS("", "")
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func handler(cfg Config) (http.Handler, error) {
	policy := mtasts.Handler()

	var webmail http.Handler
	if cfg.WebmailUpstream != "" {
		target, err := url.Parse(cfg.WebmailUpstream)
		if err != nil || target.Scheme != "http" || target.Host == "" {
			return nil, fmt.Errorf("frontdoor: invalid webmail upstream %q (expected e.g. http://127.0.0.1:8081)", cfg.WebmailUpstream)
		}
		webmail = &httputil.ReverseProxy{
			Rewrite: func(r *httputil.ProxyRequest) {
				r.SetURL(target)
				// Keep the public Host: the webmail builds absolute URLs and checks
				// Origin against it. SetXForwarded replaces any client-supplied
				// X-Forwarded-* headers, so the client IP the backend logs (and
				// fail2ban acts on) can't be spoofed.
				r.Out.Host = r.In.Host
				r.SetXForwarded()
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				log.Printf("frontdoor: webmail upstream: %v", err)
				http.Error(w, "Webmail is temporarily unavailable.", http.StatusBadGateway)
			},
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.IndexByte(host, ':'); i != -1 {
			host = host[:i]
		}
		switch {
		case mtasts.IsPolicyHost(host):
			policy.ServeHTTP(w, r)
		case cfg.API != nil && isPublicAPIPath(r.URL.Path, webmail != nil):
			cfg.API.ServeHTTP(w, r)
		case webmail != nil:
			webmail.ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}), nil
}

// isPublicAPIPath reports whether path belongs to the public API: /api/<segment
// containing a dot>/... (see the package comment). With no webmail configured, every
// /api/ path goes to the API, so mistakes get a JSON error rather than a bare 404.
func isPublicAPIPath(path string, haveWebmail bool) bool {
	rest, ok := strings.CutPrefix(path, "/api/")
	if !ok {
		return false
	}
	if !haveWebmail {
		return true
	}
	segment, _, _ := strings.Cut(rest, "/")
	return strings.Contains(segment, ".")
}
