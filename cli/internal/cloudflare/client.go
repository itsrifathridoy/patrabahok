// Package cloudflare talks to the Cloudflare API (v4) to automatically create the DNS
// records a domain needs for mail, when that domain's zone is hosted on a connected
// Cloudflare account — an alternative to the manual copy-the-records-yourself flow.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://api.cloudflare.com/client/v4"

type Client struct {
	token string
	http  *http.Client
}

func New(token string) *Client {
	return &Client{token: token, http: &http.Client{Timeout: 15 * time.Second}}
}

type apiMeta struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare API request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var meta apiMeta
	_ = json.Unmarshal(raw, &meta)
	if !meta.Success {
		if len(meta.Errors) > 0 {
			return fmt.Errorf("cloudflare API error: %s (code %d)", meta.Errors[0].Message, meta.Errors[0].Code)
		}
		return fmt.Errorf("cloudflare API request failed (HTTP %d)", resp.StatusCode)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode cloudflare response: %w", err)
		}
	}
	return nil
}

// VerifyToken confirms the token is valid and currently active.
func (c *Client) VerifyToken(ctx context.Context) error {
	var out struct {
		Result struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodGet, "/user/tokens/verify", nil, &out); err != nil {
		return err
	}
	if out.Result.Status != "active" {
		return fmt.Errorf("token status is %q, expected active", out.Result.Status)
	}
	return nil
}

type zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// FindZone looks for a Cloudflare zone matching domain or one of its parent domains
// (e.g. "school.example.com" -> tries "school.example.com", then "example.com"), the
// same way Cloudflare itself resolves an FQDN to a zone. found is false, with no error,
// if this account has no zone matching anywhere up the label chain.
func (c *Client) FindZone(ctx context.Context, domain string) (zoneID, zoneName string, found bool, err error) {
	labels := strings.Split(strings.TrimSuffix(domain, "."), ".")
	for start := 0; start <= len(labels)-2; start++ {
		candidate := strings.Join(labels[start:], ".")
		var out struct {
			Result []zone `json:"result"`
		}
		q := "/zones?name=" + url.QueryEscape(candidate)
		if err := c.do(ctx, http.MethodGet, q, nil, &out); err != nil {
			return "", "", false, err
		}
		if len(out.Result) > 0 {
			return out.Result[0].ID, out.Result[0].Name, true, nil
		}
	}
	return "", "", false, nil
}

type DNSRecord struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Proxied  bool   `json:"proxied"`
}

func (c *Client) listRecords(ctx context.Context, zoneID, recordType, name string) ([]DNSRecord, error) {
	var out struct {
		Result []DNSRecord `json:"result"`
	}
	q := fmt.Sprintf("/zones/%s/dns_records?type=%s&name=%s", zoneID, url.QueryEscape(recordType), url.QueryEscape(name))
	if err := c.do(ctx, http.MethodGet, q, nil, &out); err != nil {
		return nil, err
	}
	return out.Result, nil
}

func (c *Client) createRecord(ctx context.Context, zoneID string, rec DNSRecord) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/zones/%s/dns_records", zoneID), rec, nil)
}

func (c *Client) updateRecord(ctx context.Context, zoneID, recordID string, rec DNSRecord) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID), rec, nil)
}
