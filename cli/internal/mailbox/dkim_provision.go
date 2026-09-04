package mailbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

// ensureDKIMAndDNSRecords generates a DKIM keypair for domain (if one doesn't already
// exist) and (re)writes the DNS records dump file that feeds it, mirroring what the
// installer's 80-dkim-dmarc-dns phase does for the domain(s) known at install time.
// That phase never runs again for domains added afterward (CLI, API, or dashboard), so
// without this a later-added domain would silently get no DKIM key and no DNS record
// text — this is what actually provisions both, right when the domain is added.
func ensureDKIMAndDNSRecords(domain string) error {
	if err := os.MkdirAll(sysinfo.DKIMDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", sysinfo.DKIMDir, err)
	}

	keyPath := filepath.Join(sysinfo.DKIMDir, domain+"."+sysinfo.Selector+".key")
	recordPath := filepath.Join(sysinfo.DKIMDir, domain+"."+sysinfo.Selector+".txt")

	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		out, err := exec.Command("rspamadm", "dkim_keygen", "-s", sysinfo.Selector, "-d", domain, "-k", keyPath).Output()
		if err != nil {
			return fmt.Errorf("rspamadm dkim_keygen: %w", err)
		}
		if err := os.WriteFile(recordPath, out, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", recordPath, err)
		}
		if user, group := rspamdOwner(); user != "" {
			_ = exec.Command("chown", user+":"+group, keyPath).Run()
		}
		if err := os.Chmod(keyPath, 0o640); err != nil {
			return fmt.Errorf("chmod %s: %w", keyPath, err)
		}
	}

	return writeDNSRecordsFile(domain, recordPath)
}

func rspamdOwner() (user, group string) {
	cfg := installerConfig()
	if v, ok := cfg["rspamd_user"].(string); ok {
		user = v
	}
	if v, ok := cfg["rspamd_group"].(string); ok {
		group = v
	}
	if group == "" {
		group = user
	}
	return user, group
}

func installerConfig() map[string]any {
	st, err := sysinfo.InstallerState()
	if err != nil {
		return nil
	}
	cfg, _ := st["config"].(map[string]any)
	return cfg
}

func writeDNSRecordsFile(domain, dkimRecordPath string) error {
	cfg := installerConfig()
	var mailHost, serverIP, adminEmail string
	if v, ok := cfg["hostname"].(string); ok {
		mailHost = v
	}
	if v, ok := cfg["server_ip"].(string); ok {
		serverIP = v
	}
	if v, ok := cfg["admin_email"].(string); ok {
		adminEmail = v
	}
	if serverIP == "" {
		serverIP = "<this-server-public-ip>"
	}
	if adminEmail == "" {
		adminEmail = "postmaster@" + domain
	}

	dkimSection, err := os.ReadFile(dkimRecordPath)
	dkimText := string(dkimSection)
	if err != nil {
		dkimText = fmt.Sprintf("(DKIM record file not found at %s — check 'rspamadm dkim_keygen' output manually.)\n", dkimRecordPath)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "DNS records required for %s (mail server: %s)\n", domain, mailHost)
	b.WriteString(strings.Repeat("=", 70) + "\n\n")
	b.WriteString("-- A record (only needed once, even with multiple domains) --\n")
	fmt.Fprintf(&b, "%s.   IN  A      %s\n\n", mailHost, serverIP)
	b.WriteString("-- MX record --\n")
	fmt.Fprintf(&b, "%s.   IN  MX  10  %s.\n\n", domain, mailHost)
	b.WriteString("-- SPF (TXT) --\n")
	fmt.Fprintf(&b, "%s.   IN  TXT    \"v=spf1 mx -all\"\n\n", domain)
	b.WriteString("-- DKIM (TXT) --\n")
	b.WriteString(dkimText)
	b.WriteString("\n-- DMARC (TXT) — start at p=none, monitor, then move to quarantine/reject --\n")
	fmt.Fprintf(&b, "_dmarc.%s.   IN  TXT    \"v=DMARC1; p=none; rua=mailto:%s\"\n\n", domain, adminEmail)
	b.WriteString("-- MTA-STS (TXT) — optional, requires you to host a policy file yourself; --\n")
	b.WriteString("-- this installer does not set up that hosting (see docs/ROADMAP.md).     --\n")
	fmt.Fprintf(&b, "_mta-sts.%s.   IN  TXT    \"v=STSv1; id=%s\"\n\n", domain, time.Now().UTC().Format("20060102150405"))

	if err := os.MkdirAll(sysinfo.DNSDumpDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", sysinfo.DNSDumpDir, err)
	}
	out := filepath.Join(sysinfo.DNSDumpDir, "patrabahok-dns-"+domain+".txt")
	if err := os.WriteFile(out, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}
