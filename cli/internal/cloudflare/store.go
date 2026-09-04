package cloudflare

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/secretkey"
)

const encKeyName = "CLOUDFLARE_ENC_KEY"

// tokenRefreshSkew is how far ahead of actual expiry an OAuth access token is treated
// as already-expired, so a call in flight doesn't get cut off mid-request.
const tokenRefreshSkew = 60 * time.Second

type Mode string

const (
	ModeNone  Mode = ""
	ModeToken Mode = "token"
	ModeOAuth Mode = "oauth"
)

type Settings struct {
	Connected        bool
	Mode             Mode
	TokenPreview     string // manual-token mode only
	OAuthClientID    string // oauth mode: safe to display, not a secret
	ConnectedAt      time.Time
	PendingOAuthAuth bool // client_id/secret saved, but consent not completed yet
}

// row mirrors the cloudflare_settings columns, all nullable except id.
type row struct {
	APITokenEnc     sql.NullString
	OAuthClientID   sql.NullString
	OAuthSecretEnc  sql.NullString
	OAuthAccessEnc  sql.NullString
	OAuthRefreshEnc sql.NullString
	OAuthExpiresAt  sql.NullTime
	ConnectedAt     time.Time
}

// Store persists a single connected Cloudflare account (a plain scoped API token, or an
// OAuth client + tokens) — see cli/internal/secretkey for how the encryption key itself
// is kept out of the database.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) getRow(ctx context.Context) (*row, error) {
	var r row
	err := s.db.QueryRowContext(ctx, `
		SELECT api_token_encrypted, oauth_client_id, oauth_client_secret_encrypted,
		       oauth_access_token_encrypted, oauth_refresh_token_encrypted, oauth_expires_at, connected_at
		FROM cloudflare_settings WHERE id = 1`).
		Scan(&r.APITokenEnc, &r.OAuthClientID, &r.OAuthSecretEnc, &r.OAuthAccessEnc, &r.OAuthRefreshEnc, &r.OAuthExpiresAt, &r.ConnectedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) Get(ctx context.Context) (*Settings, error) {
	r, err := s.getRow(ctx)
	if err != nil {
		return nil, err
	}
	if r == nil {
		return &Settings{}, nil
	}

	if r.OAuthAccessEnc.Valid {
		return &Settings{Connected: true, Mode: ModeOAuth, OAuthClientID: r.OAuthClientID.String, ConnectedAt: r.ConnectedAt}, nil
	}
	if r.OAuthClientID.Valid {
		return &Settings{Connected: false, Mode: ModeOAuth, OAuthClientID: r.OAuthClientID.String, PendingOAuthAuth: true, ConnectedAt: r.ConnectedAt}, nil
	}
	if r.APITokenEnc.Valid {
		token, err := decrypt(r.APITokenEnc.String)
		if err != nil {
			return nil, fmt.Errorf("decrypt stored Cloudflare token: %w", err)
		}
		preview := token
		if len(preview) > 6 {
			preview = "…" + preview[len(preview)-6:]
		}
		return &Settings{Connected: true, Mode: ModeToken, TokenPreview: preview, ConnectedAt: r.ConnectedAt}, nil
	}
	return &Settings{}, nil
}

// Token returns a currently-valid Bearer token for the Cloudflare API — the manual API
// token as-is, or a valid (refreshing if necessary) OAuth access token — or "" if
// nothing is connected. Callers never need to know which mode is active.
func (s *Store) Token(ctx context.Context) (string, error) {
	r, err := s.getRow(ctx)
	if err != nil || r == nil {
		return "", err
	}

	if r.OAuthAccessEnc.Valid {
		if r.OAuthExpiresAt.Valid && time.Now().Add(tokenRefreshSkew).Before(r.OAuthExpiresAt.Time) {
			return decrypt(r.OAuthAccessEnc.String)
		}
		if !r.OAuthRefreshEnc.Valid || !r.OAuthClientID.Valid || !r.OAuthSecretEnc.Valid {
			return "", errors.New("cloudflare OAuth token expired and no refresh token is stored — reconnect in Settings")
		}
		clientSecret, err := decrypt(r.OAuthSecretEnc.String)
		if err != nil {
			return "", err
		}
		refreshToken, err := decrypt(r.OAuthRefreshEnc.String)
		if err != nil {
			return "", err
		}
		oc := OAuthClient{ClientID: r.OAuthClientID.String, ClientSecret: clientSecret}
		result, err := oc.RefreshAccessToken(ctx, refreshToken)
		if err != nil {
			return "", fmt.Errorf("refresh cloudflare OAuth token: %w", err)
		}
		if result.RefreshToken == "" {
			result.RefreshToken = refreshToken // some providers omit it on refresh; keep the old one
		}
		if err := s.saveOAuthTokens(ctx, result); err != nil {
			return "", err
		}
		return result.AccessToken, nil
	}

	if r.APITokenEnc.Valid {
		return decrypt(r.APITokenEnc.String)
	}
	return "", nil
}

// SetAPIToken connects (or replaces the connection) using a manually pasted, scoped API
// token — clears any OAuth client/tokens that were configured instead.
func (s *Store) SetAPIToken(ctx context.Context, token string) error {
	enc, err := encrypt(token)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO cloudflare_settings
			(id, api_token_encrypted, oauth_client_id, oauth_client_secret_encrypted,
			 oauth_access_token_encrypted, oauth_refresh_token_encrypted, oauth_expires_at, connected_at)
		VALUES (1, ?, NULL, NULL, NULL, NULL, NULL, NOW())
		ON DUPLICATE KEY UPDATE
			api_token_encrypted = VALUES(api_token_encrypted),
			oauth_client_id = NULL, oauth_client_secret_encrypted = NULL,
			oauth_access_token_encrypted = NULL, oauth_refresh_token_encrypted = NULL, oauth_expires_at = NULL,
			connected_at = VALUES(connected_at)`,
		enc)
	return err
}

// SetOAuthClient saves the admin's own Cloudflare OAuth client credentials — the first
// step of the OAuth flow, before they've actually authorized anything. Clears any
// manual API token that was configured instead.
func (s *Store) SetOAuthClient(ctx context.Context, clientID, clientSecret string) error {
	encSecret, err := encrypt(clientSecret)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO cloudflare_settings
			(id, api_token_encrypted, oauth_client_id, oauth_client_secret_encrypted,
			 oauth_access_token_encrypted, oauth_refresh_token_encrypted, oauth_expires_at, connected_at)
		VALUES (1, NULL, ?, ?, NULL, NULL, NULL, NOW())
		ON DUPLICATE KEY UPDATE
			api_token_encrypted = NULL,
			oauth_client_id = VALUES(oauth_client_id), oauth_client_secret_encrypted = VALUES(oauth_client_secret_encrypted),
			oauth_access_token_encrypted = NULL, oauth_refresh_token_encrypted = NULL, oauth_expires_at = NULL,
			connected_at = VALUES(connected_at)`,
		clientID, encSecret)
	return err
}

// OAuthClientFor returns the OAuth client config needed to build the authorize URL or
// exchange a code, using redirectURI (computed by the caller from this server's own
// hostname, since it must match what's registered on the Cloudflare OAuth client
// exactly). ok is false if no OAuth client has been configured yet.
func (s *Store) OAuthClientFor(ctx context.Context, redirectURI string) (OAuthClient, bool, error) {
	r, err := s.getRow(ctx)
	if err != nil || r == nil || !r.OAuthClientID.Valid || !r.OAuthSecretEnc.Valid {
		return OAuthClient{}, false, err
	}
	secret, err := decrypt(r.OAuthSecretEnc.String)
	if err != nil {
		return OAuthClient{}, false, err
	}
	return OAuthClient{ClientID: r.OAuthClientID.String, ClientSecret: secret, RedirectURI: redirectURI}, true, nil
}

func (s *Store) saveOAuthTokens(ctx context.Context, t *TokenResult) error {
	encAccess, err := encrypt(t.AccessToken)
	if err != nil {
		return err
	}
	encRefresh, err := encrypt(t.RefreshToken)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE cloudflare_settings
		SET oauth_access_token_encrypted = ?, oauth_refresh_token_encrypted = ?, oauth_expires_at = ?
		WHERE id = 1`,
		encAccess, encRefresh, t.ExpiresAt)
	return err
}

// SaveOAuthCallback stores the tokens from a completed authorization-code exchange,
// marking the connection as fully live (not just pending).
func (s *Store) SaveOAuthCallback(ctx context.Context, t *TokenResult) error {
	return s.saveOAuthTokens(ctx, t)
}

func (s *Store) Clear(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cloudflare_settings WHERE id = 1`)
	return err
}

func encrypt(plaintext string) (string, error) {
	key, err := secretkey.Ensure(encKeyName)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decrypt(encoded string) (string, error) {
	key, err := secretkey.Ensure(encKeyName)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
