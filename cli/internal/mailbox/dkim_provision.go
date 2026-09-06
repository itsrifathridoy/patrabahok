package mailbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/dnscheck"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mtasts"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

// dkimKeyBits: RFC 8301 recommends 2048-bit RSA for DKIM; rspamadm dkim_keygen's own
// default without -b is 1024-bit, which is weak by current standards (and increasingly
// flagged/rejected by major mailbox providers) — always pass this explicitly rather than
// rely on it. Mirrors lib/phases/80-dkim-dmarc-dns.sh's DKIM_KEY_BITS.
const dkimKeyBits = "2048"

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
		out, err := exec.Command("rspamadm", "dkim_keygen", "-s", sysinfo.Selector, "-d", domain, "-b", dkimKeyBits, "-k", keyPath).Output()
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

	// Runs every call, not just on fresh generation, so a domain whose record file was
	// written before this fix existed gets self-healed the next time it's touched (e.g.
	// a repeat `domain add`), without needing a separate migration step.
	if err := normalizeDKIMRecordName(recordPath, domain); err != nil {
		return fmt.Errorf("normalize DKIM record name: %w", err)
	}
	if err := normalizeDKIMRecordQuoting(recordPath); err != nil {
		return fmt.Errorf("normalize DKIM record quoting: %w", err)
	}

	if mailHost := mailHostFromConfig(); mailHost != "" {
		if _, _, err := mtasts.WritePolicy(domain, mailHost); err != nil {
			return fmt.Errorf("write MTA-STS policy: %w", err)
		}
	}

	return writeDNSRecordsFile(domain, recordPath)
}

// RotateDKIMKey deletes domain's current DKIM key/record files and regenerates them at
// the current dkimKeyBits size — the actual, usable way to move an already-provisioned
// domain onto a new key size (ensureDKIMAndDNSRecords deliberately never overwrites an
// existing key file on its own, since silently rotating keys on every routine
// `domain add` would be dangerous). Restarts rspamd afterward: unlike a brand-new
// domain's key (which rspamd's dkim_signing module reads lazily and has never seen
// before), it's not guaranteed rspamd won't keep signing with an already-loaded key
// object for one it has — a restart forces it to read fresh from disk either way.
//
// The DNS side is NOT automatic: the old public key stays published (and outgoing mail
// keeps verifying against the old key, since callers must republish the new DKIM DNS
// record themselves — e.g. via Cloudflare auto-configure — as this changes which value
// buildRecordEntries/dnscheck compare against.
func RotateDKIMKey(domain string) error {
	if err := ValidateDomain(domain); err != nil {
		return err
	}
	keyPath := filepath.Join(sysinfo.DKIMDir, domain+"."+sysinfo.Selector+".key")
	recordPath := filepath.Join(sysinfo.DKIMDir, domain+"."+sysinfo.Selector+".txt")
	if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing key %s: %w", keyPath, err)
	}
	if err := os.Remove(recordPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove existing record %s: %w", recordPath, err)
	}
	if err := ensureDKIMAndDNSRecords(domain); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "restart", "rspamd").Run()
	return nil
}

func mailHostFromConfig() string {
	cfg := installerConfig()
	if v, ok := cfg["hostname"].(string); ok {
		return v
	}
	return ""
}

// normalizeDKIMRecordName rewrites the DKIM record file so it starts with the fully
// qualified name (selector._domainkey.<domain>) instead of rspamadm dkim_keygen's bare,
// zone-file-relative selector ("mail._domainkey"). The bare form is only meaningful
// inside a zone file that already has $ORIGIN set to this exact domain — it's wrong to
// paste as-is into a DNS provider's "Name" field, or to use as a Cloudflare API record
// name (which internal/cloudflare already builds separately and correctly; this only
// affects the human-readable text shown for manual copy-paste).
func normalizeDKIMRecordName(recordPath, domain string) error {
	data, err := os.ReadFile(recordPath)
	if err != nil {
		return err
	}
	text := string(data)
	qualified := sysinfo.Selector + "._domainkey." + domain
	bare := sysinfo.Selector + "._domainkey"
	if strings.HasPrefix(text, qualified) {
		return nil // already fixed
	}
	if !strings.HasPrefix(text, bare) {
		return nil // unrecognized format — leave it alone rather than guess
	}
	text = qualified + text[len(bare):]
	return os.WriteFile(recordPath, []byte(text), 0o644)
}

// dkimCharStringMax is the DNS wire-format limit for a single TXT <character-string>
// (RFC 1035 §3.3: a length-prefixed byte, so 255 is the hard ceiling) — not a stylistic
// choice.
const dkimCharStringMax = 255

// normalizeDKIMRecordQuoting rewrites the DKIM record file's TXT value into as few
// 255-byte-max quoted segments as the DNS wire format actually requires, instead of
// keeping whatever fixed split `rspamadm dkim_keygen` happened to use in its zone-file
// output (always two segments, "v=DKIM1; k=rsa;" and "p=...", regardless of whether the
// combined value is anywhere near the 255-byte limit). For a 1024-bit RSA key the whole
// value is under 255 bytes, so this collapses it to one quoted segment — confirmed via a
// live `dig` query to match exactly what Cloudflare's API already stores on the wire,
// and what a DNS provider's single "Value" field expects when copy-pasted by hand,
// rather than the two-segment zone-file text a user would otherwise paste verbatim
// (quotes, embedded line break, and all) into a field that wants one continuous string.
// Stays correct automatically if a larger key ever needs genuinely more than one
// 255-byte segment — this only ever produces the minimum segment count required.
// Runs every time (not just after a fresh generation), so an already-generated record
// file gets self-healed the next time it's touched, same as normalizeDKIMRecordName.
func normalizeDKIMRecordQuoting(recordPath string) error {
	data, err := os.ReadFile(recordPath)
	if err != nil {
		return err
	}
	text := string(data)
	if strings.Count(text, `"`) <= 2 {
		return nil // already a single quoted segment (or an unrecognized format — leave it alone)
	}
	firstQuote := strings.IndexByte(text, '"')
	lastQuote := strings.LastIndexByte(text, '"')
	if firstQuote == -1 || lastQuote == firstQuote {
		return nil // no complete quoted segment found — leave it alone rather than guess
	}
	prefix := text[:firstQuote]
	suffix := text[lastQuote+1:]
	value := dnscheck.FullDKIMRecordValue(text)
	if value == "" {
		return nil
	}

	var chunks []string
	for len(value) > 0 {
		n := dkimCharStringMax
		if n > len(value) {
			n = len(value)
		}
		chunks = append(chunks, `"`+value[:n]+`"`)
		value = value[n:]
	}

	newText := prefix + strings.Join(chunks, " ") + suffix
	return os.WriteFile(recordPath, []byte(newText), 0o644)
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
	b.WriteString("-- MTA-STS (TXT + A, optional) — add both records, then run --\n")
	b.WriteString("-- 'patrabahok mta-sts enable " + domain + "' (or use the dashboard's DNS --\n")
	b.WriteString("-- Analysis page) to actually issue the certificate and start hosting --\n")
	b.WriteString("-- the policy file this record points at.                             --\n")
	fmt.Fprintf(&b, "%s.   IN  A      %s\n", mtasts.Hostname(domain), serverIP)
	stsContent := mtasts.PolicyContent(mailHost)
	fmt.Fprintf(&b, "_mta-sts.%s.   IN  TXT    \"v=STSv1; id=%s\"\n\n", domain, mtasts.PolicyID(stsContent))

	if err := os.MkdirAll(sysinfo.DNSDumpDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", sysinfo.DNSDumpDir, err)
	}
	out := filepath.Join(sysinfo.DNSDumpDir, "patrabahok-dns-"+domain+".txt")
	if err := os.WriteFile(out, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	return nil
}
