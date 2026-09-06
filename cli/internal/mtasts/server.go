package mtasts

import (
	"crypto/tls"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// multiCertReloader is certReloader (cli/internal/webui/server.go) generalized to many
// certificates picked by SNI server name instead of one fixed cert/key pair — MTA-STS
// hosting needs a different certificate per domain (mta-sts.<domain>), all served off
// the same :443 listener, and new domains get enabled without a restart.
type multiCertReloader struct {
	mu    sync.Mutex
	byKey map[string]*perNameCert
}

type perNameCert struct {
	cert    *tls.Certificate
	modTime time.Time
}

func newMultiCertReloader() *multiCertReloader {
	return &multiCertReloader{byKey: make(map[string]*perNameCert)}
}

func (m *multiCertReloader) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := strings.ToLower(strings.TrimSuffix(hello.ServerName, "."))
	if name == "" || !strings.HasPrefix(name, "mta-sts.") {
		return nil, errNoCert
	}
	certPath := certDir(strings.TrimPrefix(name, "mta-sts."))
	fullchain := certPath + "/fullchain.pem"
	keyPath := certPath + "/privkey.pem"

	m.mu.Lock()
	defer m.mu.Unlock()

	info, err := os.Stat(fullchain)
	if err != nil {
		return nil, errNoCert
	}
	entry, ok := m.byKey[name]
	if !ok || info.ModTime().After(entry.modTime) {
		cert, err := tls.LoadX509KeyPair(fullchain, keyPath)
		if err != nil {
			if ok {
				return entry.cert, nil // serve the last-known-good cert rather than fail an in-progress renewal
			}
			return nil, errNoCert
		}
		entry = &perNameCert{cert: &cert, modTime: info.ModTime()}
		m.byKey[name] = entry
	}
	return entry.cert, nil
}

type notFoundError struct{ s string }

func (e *notFoundError) Error() string { return e.s }

var errNoCert = &notFoundError{"mtasts: no certificate for this name"}

// Handler serves exactly GET /.well-known/mta-sts.txt, resolving the requested domain
// from the Host header (SNI already restricted the TLS connection to a name this server
// actually holds a certificate for, but the handler re-derives it independently rather
// than trusting that coupling). Everything else — wrong path, unknown domain, a domain
// whose policy was never enabled — is a 404, deliberately with no further detail: this
// endpoint is fetched unauthenticated by any mail server in the world that wants to send
// this domain mail, not a place to leak internal state.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if i := strings.IndexByte(host, ':'); i != -1 {
			host = host[:i]
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path != "/.well-known/mta-sts.txt" || !strings.HasPrefix(host, "mta-sts.") {
			http.NotFound(w, r)
			return
		}
		domain := strings.TrimPrefix(host, "mta-sts.")
		data, err := os.ReadFile(policyPath(domain))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(data)
	})
}

// ListenAndServeTLS starts the MTA-STS policy-hosting listener on addr (":443" in
// production). Every certificate is picked per-connection by SNI (multiCertReloader),
// so domains can be enabled and their certificates renewed without a restart.
func ListenAndServeTLS(addr string) error {
	reloader := newMultiCertReloader()
	srv := &http.Server{
		Addr:         addr,
		Handler:      Handler(),
		TLSConfig:    &tls.Config{GetCertificate: reloader.GetCertificate, MinVersion: tls.VersionTLS12},
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		// Silences per-connection handshake-failure logging on the default logger: :443
		// draws constant unsolicited TLS/SNI probing from internet-wide scanners that
		// will never present a "mta-sts.*" SNI, and every one of those is otherwise a log
		// line for a rejection that isn't a real operational problem.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	log.Printf("mtasts: listening on https://%s (serving enabled domains' /.well-known/mta-sts.txt)", addr)
	return srv.ListenAndServeTLS("", "")
}
