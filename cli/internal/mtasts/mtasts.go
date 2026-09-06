// Package mtasts hosts the HTTPS policy file MTA-STS (RFC 8461) requires at
// https://mta-sts.<domain>/.well-known/mta-sts.txt, and enables it for a domain: writing
// that policy file and issuing/renewing the Let's Encrypt certificate mta-sts.<domain>
// needs to be served over TLS. The installer already prints the DNS side of MTA-STS
// (the _mta-sts TXT record); this is what actually makes the record true instead of
// aspirational.
package mtasts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

// PolicyDir is where per-domain policy files live — a sibling of sysinfo.DNSDumpDir,
// readable by patrabahokd's systemd sandbox (ProtectSystem=strict blocks writes outside
// ReadWritePaths, not reads) without any unit-file change needed.
const PolicyDir = "/var/lib/patrabahok/mta-sts"

// Hostname returns the hostname MTA-STS policy hosting for domain must be reachable at.
func Hostname(domain string) string { return "mta-sts." + domain }

// PolicyContent builds the policy file content. mode is "testing": failures are
// reported (if the sender supports TLSRPT) but never block delivery — the same
// conservative starting point this project already uses for DMARC's p=none, left for
// the admin to tighten once they've confirmed nothing is actually relying on a
// non-MX/non-TLS delivery path.
func PolicyContent(mailHost string) string {
	return fmt.Sprintf("version: STSv1\nmode: testing\nmx: %s\nmax_age: 604800\n", mailHost)
}

// PolicyID derives the DNS record's "id" field from the policy content, so it only
// changes when the policy actually does — required by RFC 8461 (senders cache the
// policy for max_age and only re-fetch when this id changes), and easy to get wrong
// with a timestamp-based id that changes on every unrelated regeneration.
func PolicyID(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

func policyPath(domain string) string {
	return filepath.Join(PolicyDir, domain, "mta-sts.txt")
}

// WritePolicy (re)writes domain's policy file from the current mail hostname, returning
// its content and derived id. Idempotent and safe to call on every domain
// add/re-provision, same as the DKIM key/DNS-dump generation it sits alongside.
func WritePolicy(domain, mailHost string) (content, id string, err error) {
	content = PolicyContent(mailHost)
	id = PolicyID(content)
	dir := filepath.Join(PolicyDir, domain)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create %s: %w", dir, err)
	}
	if err := os.WriteFile(policyPath(domain), []byte(content), 0o644); err != nil {
		return "", "", fmt.Errorf("write policy for %s: %w", domain, err)
	}
	return content, id, nil
}

// HasPolicy reports whether a policy file has been written for domain.
func HasPolicy(domain string) bool {
	_, err := os.Stat(policyPath(domain))
	return err == nil
}

func certDir(domain string) string {
	return filepath.Join("/etc/letsencrypt/live", Hostname(domain))
}

// HasCert reports whether a Let's Encrypt certificate has been issued for
// mta-sts.<domain>.
func HasCert(domain string) bool {
	_, err := os.Stat(filepath.Join(certDir(domain), "fullchain.pem"))
	return err == nil
}

// Enabled reports whether MTA-STS hosting is actually live for domain: a policy file to
// serve and a certificate to serve it over TLS with.
func Enabled(domain string) bool {
	return HasPolicy(domain) && HasCert(domain)
}

func installerConfig() map[string]any {
	st, err := sysinfo.InstallerState()
	if err != nil {
		return nil
	}
	cfg, _ := st["config"].(map[string]any)
	return cfg
}

// StateConfigStrings reads the mail hostname, server IP, and admin email the installer
// recorded, the same three values every other DNS/cert operation in this codebase reads
// from state.json.
func StateConfigStrings() (mailHost, serverIP, adminEmail string) {
	cfg := installerConfig()
	if cfg == nil {
		return "", "", ""
	}
	if v, ok := cfg["hostname"].(string); ok {
		mailHost = v
	}
	if v, ok := cfg["server_ip"].(string); ok {
		serverIP = v
	}
	if v, ok := cfg["admin_email"].(string); ok {
		adminEmail = v
	}
	return
}

// Enable turns on MTA-STS hosting for domain: verifies mta-sts.<domain> actually
// resolves to this server first (a Let's Encrypt HTTP-01 challenge will otherwise fail
// with a much more confusing error), writes the policy file, and issues/renews its
// certificate via certbot standalone on port 80 — the same mechanism 40-tls.sh uses for
// the mail hostname's own certificate, and, like that one, safe to call repeatedly
// (--keep-until-expiring skips re-issuance of a still-valid certificate without any
// network call). Once issued, certbot's own renewal timer (already enabled by the
// installer) picks it up automatically; patrabahokd's certificate loader re-reads it
// from disk on its next TLS handshake once the file's mtime changes, no restart needed.
func Enable(ctx context.Context, domain string) (id string, err error) {
	mailHost, serverIP, adminEmail := StateConfigStrings()
	if mailHost == "" || serverIP == "" {
		return "", fmt.Errorf("installer state is missing hostname/server_ip — is this a fully installed server?")
	}
	if adminEmail == "" {
		adminEmail = "postmaster@" + domain
	}

	host := Hostname(domain)
	if err := checkResolvesTo(ctx, host, serverIP); err != nil {
		return "", err
	}

	_, id, err = WritePolicy(domain, mailHost)
	if err != nil {
		return "", err
	}

	// Defensive, mirroring 40-tls.sh: nothing in this stack normally listens on port 80,
	// but certbot --standalone needs it free for the few seconds the HTTP-01 challenge
	// takes.
	_ = exec.Command("systemctl", "stop", "nginx", "apache2").Run()

	certCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(certCtx, "certbot", "certonly", "--standalone", "--non-interactive", "--agree-tos",
		"-m", adminEmail, "-d", host, "--cert-name", host, "--keep-until-expiring")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("certbot failed for %s: %w\n%s", host, err, strings.TrimSpace(string(out)))
	}

	return id, nil
}

// checkResolvesTo fails fast with a clear, specific message when the DNS side of MTA-STS
// hosting isn't ready yet, instead of letting certbot's HTTP-01 challenge fail and
// surfacing a generic ACME error that doesn't point at the actual cause.
//
// Retries a few times before giving up: live testing against a real domain found this
// single-shot lookup intermittently erroring on an otherwise-fine record — a transient
// resolver hiccup (a dropped UDP query, a slow parallel AAAA lookup with no reply at
// all) reported as "does not resolve yet" even though a plain `dig` right alongside it
// succeeded every time. One retry is not enough to be confident it's a real problem
// with the record rather than the query.
func checkResolvesTo(ctx context.Context, host, expectedIP string) error {
	const attempts = 4
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("%s does not resolve yet — add an A record for it pointing to %s, wait for it to propagate, then try again", host, expectedIP)
			case <-time.After(2 * time.Second):
			}
		}
		lookupCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		ips, err := net.DefaultResolver.LookupHost(lookupCtx, host)
		cancel()
		if err != nil || len(ips) == 0 {
			continue
		}
		for _, ip := range ips {
			if ip == expectedIP {
				return nil
			}
		}
		// Resolved, just not to us yet — a real mismatch, no point retrying.
		return fmt.Errorf("%s resolves to %s, not this server (%s) — fix its A record, wait for it to propagate, then try again", host, strings.Join(ips, ", "), expectedIP)
	}
	return fmt.Errorf("%s does not resolve yet — add an A record for it pointing to %s, wait for it to propagate, then try again", host, expectedIP)
}
