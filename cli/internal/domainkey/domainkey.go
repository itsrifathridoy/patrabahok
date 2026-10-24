// Package domainkey manages the public API's per-domain API keys: at most one key per
// domain, granting management of that domain's mailboxes and aliases only (see
// internal/publicapi). Same storage model as internal/authtoken — only a SHA-256 hash is
// kept, the plaintext is shown once at generation time — and the same reasoning for a
// fast hash: keys are 256-bit random values, not guessable human-chosen secrets.
package domainkey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const keyPrefix = "pbd_"

var ErrInvalidKey = errors.New("invalid or revoked API key")

type KeyInfo struct {
	Domain     string
	Hint       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func hashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// Generate creates a new API key for domain, replacing (and so immediately revoking)
// any key it already had. The caller must show the returned plaintext to the user right
// away — it is never stored and can't be recovered later.
func (s *Store) Generate(ctx context.Context, domain string) (string, error) {
	var domainID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM virtual_domains WHERE name = ?`, domain).Scan(&domainID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("domain %q is not registered", domain)
	}
	if err != nil {
		return "", err
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	plaintext := keyPrefix + hex.EncodeToString(b)
	hint := plaintext[len(plaintext)-6:]

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO domain_api_keys (domain_id, key_hash, key_hint) VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE key_hash = VALUES(key_hash), key_hint = VALUES(key_hint),
		                        created_at = CURRENT_TIMESTAMP, last_used_at = NULL`,
		domainID, hashKey(plaintext), hint)
	if err != nil {
		return "", err
	}
	return plaintext, nil
}

// List returns the key metadata for every domain that currently has a key.
func (s *Store) List(ctx context.Context) ([]KeyInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT vd.name, k.key_hint, k.created_at, k.last_used_at
		FROM domain_api_keys k JOIN virtual_domains vd ON vd.id = k.domain_id
		ORDER BY vd.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KeyInfo
	for rows.Next() {
		var k KeyInfo
		var lastUsed sql.NullTime
		if err := rows.Scan(&k.Domain, &k.Hint, &k.CreatedAt, &lastUsed); err != nil {
			return nil, err
		}
		if lastUsed.Valid {
			k.LastUsedAt = &lastUsed.Time
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) Revoke(ctx context.Context, domain string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE k FROM domain_api_keys k JOIN virtual_domains vd ON vd.id = k.domain_id
		WHERE vd.name = ?`, domain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("domain %q has no API key", domain)
	}
	return nil
}

// Verify returns the domain a presented key belongs to, and records its use.
func (s *Store) Verify(ctx context.Context, plaintext string) (string, error) {
	if !strings.HasPrefix(plaintext, keyPrefix) {
		return "", ErrInvalidKey
	}
	var domainID int64
	var domain string
	err := s.db.QueryRowContext(ctx, `
		SELECT k.domain_id, vd.name FROM domain_api_keys k
		JOIN virtual_domains vd ON vd.id = k.domain_id
		WHERE k.key_hash = ?`, hashKey(plaintext)).Scan(&domainID, &domain)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidKey
	}
	if err != nil {
		return "", err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE domain_api_keys SET last_used_at = NOW() WHERE domain_id = ?`, domainID)
	return domain, nil
}
