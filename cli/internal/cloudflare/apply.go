package cloudflare

import (
	"context"
	"strings"
)

type ApplyResult struct {
	Label  string
	Action string // "created", "updated", "unchanged", "skipped", "error"
	Detail string
	Err    error
}

// ApplyMailRecords creates or updates the DNS records a domain needs to send and
// receive mail, in the given Cloudflare zone. Idempotent and conservative: a record
// that already matches is left alone, and nothing outside these specific records is
// ever touched or deleted — e.g. other unrelated TXT records at the domain apex, or
// other MX records already pointing elsewhere, are never removed.
func ApplyMailRecords(ctx context.Context, c *Client, zoneID, domain, mailHost, serverIP, dkimValue, dmarcValue string) []ApplyResult {
	return []ApplyResult{
		upsertSingleHost(ctx, c, zoneID, "A record", "A", mailHost, serverIP, 0),
		upsertMX(ctx, c, zoneID, domain, mailHost),
		upsertTXTByPrefix(ctx, c, zoneID, "SPF", domain, "v=spf1", "v=spf1 mx -all"),
		upsertTXTByPrefix(ctx, c, zoneID, "DMARC", "_dmarc."+domain, "v=DMARC1", dmarcValue),
		upsertTXTByPrefix(ctx, c, zoneID, "DKIM", "mail._domainkey."+domain, "v=DKIM1", dkimValue),
	}
}

func upsertSingleHost(ctx context.Context, c *Client, zoneID, label, recordType, name, content string, priority int) ApplyResult {
	existing, err := c.listRecords(ctx, zoneID, recordType, name)
	if err != nil {
		return ApplyResult{Label: label, Action: "error", Err: err}
	}
	for _, r := range existing {
		if r.Content == content {
			return ApplyResult{Label: label, Action: "unchanged", Detail: content}
		}
	}
	if len(existing) == 1 {
		rec := existing[0]
		rec.Content = content
		if err := c.updateRecord(ctx, zoneID, rec.ID, rec); err != nil {
			return ApplyResult{Label: label, Action: "error", Err: err}
		}
		return ApplyResult{Label: label, Action: "updated", Detail: content}
	}
	if len(existing) > 1 {
		return ApplyResult{Label: label, Action: "skipped", Detail: "multiple existing " + recordType + " records at " + name + " already — add/fix manually"}
	}
	if err := c.createRecord(ctx, zoneID, DNSRecord{Type: recordType, Name: name, Content: content, TTL: 300, Priority: priority}); err != nil {
		return ApplyResult{Label: label, Action: "error", Err: err}
	}
	return ApplyResult{Label: label, Action: "created", Detail: content}
}

func upsertMX(ctx context.Context, c *Client, zoneID, domain, mailHost string) ApplyResult {
	existing, err := c.listRecords(ctx, zoneID, "MX", domain)
	if err != nil {
		return ApplyResult{Label: "MX record", Action: "error", Err: err}
	}
	target := strings.TrimSuffix(mailHost, ".")
	for _, r := range existing {
		if strings.EqualFold(strings.TrimSuffix(r.Content, "."), target) {
			return ApplyResult{Label: "MX record", Action: "unchanged", Detail: mailHost}
		}
	}
	if err := c.createRecord(ctx, zoneID, DNSRecord{Type: "MX", Name: domain, Content: mailHost, TTL: 300, Priority: 10}); err != nil {
		return ApplyResult{Label: "MX record", Action: "error", Err: err}
	}
	return ApplyResult{Label: "MX record", Action: "created", Detail: mailHost}
}

// upsertTXTByPrefix only ever looks at (and touches) TXT records already starting with
// prefix, so it never disturbs unrelated TXT records that happen to share the same
// name (e.g. a domain-verification TXT record sitting at the apex alongside SPF).
func upsertTXTByPrefix(ctx context.Context, c *Client, zoneID, label, name, prefix, content string) ApplyResult {
	existing, err := c.listRecords(ctx, zoneID, "TXT", name)
	if err != nil {
		return ApplyResult{Label: label, Action: "error", Err: err}
	}
	var matches []DNSRecord
	for _, r := range existing {
		if strings.HasPrefix(strings.Trim(r.Content, `"`), prefix) {
			matches = append(matches, r)
		}
	}
	for _, r := range matches {
		if r.Content == content {
			return ApplyResult{Label: label, Action: "unchanged", Detail: content}
		}
	}
	if len(matches) == 1 {
		rec := matches[0]
		rec.Content = content
		if err := c.updateRecord(ctx, zoneID, rec.ID, rec); err != nil {
			return ApplyResult{Label: label, Action: "error", Err: err}
		}
		return ApplyResult{Label: label, Action: "updated", Detail: content}
	}
	if len(matches) > 1 {
		return ApplyResult{Label: label, Action: "skipped", Detail: "multiple existing " + prefix + " records at " + name + " already — add/fix manually"}
	}
	if err := c.createRecord(ctx, zoneID, DNSRecord{Type: "TXT", Name: name, Content: content, TTL: 300}); err != nil {
		return ApplyResult{Label: label, Action: "error", Err: err}
	}
	return ApplyResult{Label: label, Action: "created", Detail: content}
}
