package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cloudflare's OAuth 2.0 endpoints (Authorization Code flow) — see
// https://developers.cloudflare.com/fundamentals/oauth/. These are account-level OAuth
// clients the admin registers themselves (Manage Account > OAuth clients) for their own
// Cloudflare account; there is no public, self-service "patrabahok" app Cloudflare
// hosts on everyone's behalf, so each self-hosted install connects via its own client.
const (
	AuthorizeURL = "https://dash.cloudflare.com/oauth2/auth"
	TokenURL     = "https://dash.cloudflare.com/oauth2/token"
	RevokeURL    = "https://dash.cloudflare.com/oauth2/revoke"
)

type OAuthClient struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
}

// AuthorizeURL builds the URL to send the admin's browser to. state must be a random,
// unguessable value the caller also stashes (e.g. in a cookie) to verify on callback —
// the standard OAuth CSRF defense, independent of session-cookie SameSite policy.
func (o OAuthClient) BuildAuthorizeURL(state string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", o.ClientID)
	v.Set("redirect_uri", o.RedirectURI)
	v.Set("state", state)
	return AuthorizeURL + "?" + v.Encode()
}

type TokenResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
}

// ExchangeCode trades an authorization code (from the callback's ?code=) for an access
// + refresh token pair.
func (o OAuthClient) ExchangeCode(ctx context.Context, code string) (*TokenResult, error) {
	return o.tokenRequest(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {o.RedirectURI},
	})
}

// RefreshAccessToken trades a previously issued refresh token for a fresh access token.
func (o OAuthClient) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenResult, error) {
	return o.tokenRequest(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	})
}

func (o OAuthClient) tokenRequest(ctx context.Context, form url.Values) (*TokenResult, error) {
	form.Set("client_id", o.ClientID)
	form.Set("client_secret", o.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloudflare OAuth token request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode cloudflare OAuth token response (HTTP %d): %w", resp.StatusCode, err)
	}
	if out.Error != "" {
		return nil, fmt.Errorf("cloudflare OAuth error: %s (%s)", out.Error, out.ErrorDesc)
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return nil, fmt.Errorf("cloudflare OAuth token request failed (HTTP %d)", resp.StatusCode)
	}

	expiresAt := time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	if out.ExpiresIn <= 0 {
		expiresAt = time.Now().Add(1 * time.Hour) // conservative fallback if omitted
	}
	return &TokenResult{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, ExpiresAt: expiresAt}, nil
}
