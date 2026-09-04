// Package dnscheck performs live DNS lookups (via the server's own resolver — the
// local unbound instance, per /etc/resolv.conf) to verify a domain's mail-related DNS
// records actually match what this server expects, rather than just printing the text
// and hoping. This is exactly the kind of check that would have caught the stale-DKIM
// bug found during manual testing: a domain whose DKIM key was regenerated but whose
// DNS record was never updated.
package dnscheck

import (
	"context"
	"net"
	"strings"
	"time"
)

type Check struct {
	Label    string
	Expected string
	Found    string
	Pass     bool
}

type Report struct {
	Domain   string
	MailHost string
	Checks   []Check
	AllPass  bool
}

func timeoutCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 8*time.Second)
}

func checkA(host, expectedIP string) Check {
	ctx, cancel := timeoutCtx()
	defer cancel()
	ips, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil || len(ips) == 0 {
		return Check{Label: "A record: " + host, Expected: expectedIP, Found: "not found", Pass: false}
	}
	for _, ip := range ips {
		if ip == expectedIP {
			return Check{Label: "A record: " + host, Expected: expectedIP, Found: strings.Join(ips, ", "), Pass: true}
		}
	}
	return Check{Label: "A record: " + host, Expected: expectedIP, Found: strings.Join(ips, ", "), Pass: false}
}

func checkMX(domain, expectedHost string) Check {
	ctx, cancel := timeoutCtx()
	defer cancel()
	mxs, err := net.DefaultResolver.LookupMX(ctx, domain)
	if err != nil || len(mxs) == 0 {
		return Check{Label: "MX record", Expected: expectedHost, Found: "not found", Pass: false}
	}
	var found []string
	pass := false
	for _, mx := range mxs {
		h := strings.TrimSuffix(mx.Host, ".")
		found = append(found, h)
		if strings.EqualFold(h, expectedHost) {
			pass = true
		}
	}
	return Check{Label: "MX record", Expected: expectedHost, Found: strings.Join(found, ", "), Pass: pass}
}

func lookupTXT(name string) ([]string, error) {
	ctx, cancel := timeoutCtx()
	defer cancel()
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func checkSPF(domain string) Check {
	txts, err := lookupTXT(domain)
	if err != nil {
		return Check{Label: "SPF", Expected: "v=spf1 ...", Found: "not found", Pass: false}
	}
	for _, t := range txts {
		if strings.HasPrefix(t, "v=spf1") {
			return Check{Label: "SPF", Expected: "v=spf1 ...", Found: t, Pass: true}
		}
	}
	return Check{Label: "SPF", Expected: "v=spf1 ...", Found: strings.Join(txts, " | "), Pass: false}
}

func checkDMARC(domain string) Check {
	txts, err := lookupTXT("_dmarc." + domain)
	if err != nil {
		return Check{Label: "DMARC", Expected: "v=DMARC1; ...", Found: "not found", Pass: false}
	}
	for _, t := range txts {
		if strings.HasPrefix(t, "v=DMARC1") {
			return Check{Label: "DMARC", Expected: "v=DMARC1; ...", Found: t, Pass: true}
		}
	}
	return Check{Label: "DMARC", Expected: "v=DMARC1; ...", Found: strings.Join(txts, " | "), Pass: false}
}

// normalizeKey strips everything but the base64 alphabet, so formatting differences
// (quotes, line splits, whitespace) between our locally-generated record and however a
// DNS provider stored it don't cause a false mismatch.
func normalizeKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '+', r == '/', r == '=':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LocalDKIMKeyFragment extracts the base64 public-key blob from our own generated DNS
// record text (the "p=..." value), independent of the quoted-string DNS zone-file
// formatting rspamadm dkim_keygen produces.
func LocalDKIMKeyFragment(rawRecordText string) string {
	var joined strings.Builder
	inQuote := false
	for _, r := range rawRecordText {
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if inQuote {
			joined.WriteRune(r)
		}
	}
	full := joined.String()
	idx := strings.Index(full, "p=")
	if idx == -1 {
		return ""
	}
	return normalizeKey(full[idx+2:])
}

func checkDKIM(domain, selector, localRecordText string) Check {
	label := "DKIM (" + selector + "._domainkey)"
	localKey := LocalDKIMKeyFragment(localRecordText)
	if localKey == "" {
		return Check{Label: label, Expected: "matches locally generated key", Found: "no local key found", Pass: false}
	}
	txts, err := lookupTXT(selector + "._domainkey." + domain)
	if err != nil || len(txts) == 0 {
		return Check{Label: label, Expected: "matches locally generated key", Found: "not found", Pass: false}
	}
	published := normalizeKey(strings.Join(txts, ""))
	pass := published != "" && strings.Contains(published, localKey)
	found := "present, key does not match this server's current key"
	if pass {
		found = "present, matches this server's key"
	}
	return Check{Label: label, Expected: "matches locally generated key", Found: found, Pass: pass}
}

// Analyze runs every check for a domain. dkimRecordText is the raw content of this
// server's own generated DKIM record (e.g. from sysinfo.DKIMRecord); pass "" to skip
// the DKIM check (e.g. if no key has been generated for this domain yet).
func Analyze(domain, mailHost, serverIP, dkimRecordText string) Report {
	var checks []Check
	if serverIP != "" && mailHost != "" {
		checks = append(checks, checkA(mailHost, serverIP))
	}
	if mailHost != "" {
		checks = append(checks, checkMX(domain, mailHost))
	}
	checks = append(checks, checkSPF(domain))
	checks = append(checks, checkDMARC(domain))
	if dkimRecordText != "" {
		checks = append(checks, checkDKIM(domain, "mail", dkimRecordText))
	}

	allPass := len(checks) > 0
	for _, c := range checks {
		if !c.Pass {
			allPass = false
			break
		}
	}
	return Report{Domain: domain, MailHost: mailHost, Checks: checks, AllPass: allPass}
}
