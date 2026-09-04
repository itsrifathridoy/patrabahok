// Package adminauth handles the web dashboard's own accounts: argon2id password
// hashing (a real KDF — these are human-chosen secrets, unlike api_tokens' random
// values) and server-side sessions identified by a random cookie value, only ever
// stored as a SHA-256 hash (the cookie itself is already high-entropy).
package adminauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	saltLen      = 16

	SessionCookieName = "patrabahok_session"
	sessionTTL        = 7 * 24 * time.Hour
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrUserExists         = errors.New("username already exists")
)

func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))
	return encoded, nil
}

func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("unrecognized password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	var mem, time_, threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &time_, &threads); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, time_, mem, uint8(threads), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

type AdminUser struct {
	ID       int64
	Username string
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) CreateUser(ctx context.Context, username, password string) error {
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO admin_users (username, password_hash) VALUES (?, ?)`, username, hash)
	if err != nil && strings.Contains(err.Error(), "Duplicate entry") {
		return ErrUserExists
	}
	return err
}

func (s *Store) ListUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username FROM admin_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Username); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&n)
	return n, err
}

func (s *Store) DeleteUser(ctx context.Context, username string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM admin_users WHERE username = ?`, username)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no admin user named %q", username)
	}
	return nil
}

func (s *Store) ChangePassword(ctx context.Context, username, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE admin_users SET password_hash = ? WHERE username = ?`, hash, username)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no admin user named %q", username)
	}
	return nil
}

// Authenticate checks credentials and records a successful login. Returns
// ErrInvalidCredentials for both "no such user" and "wrong password" — deliberately
// not distinguishing the two in the returned error, to avoid username enumeration.
func (s *Store) Authenticate(ctx context.Context, username, password string) (*AdminUser, error) {
	var u AdminUser
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT id, username, password_hash FROM admin_users WHERE username = ?`, username).
		Scan(&u.ID, &u.Username, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Still run a hash verification against a dummy value so this code path
		// takes roughly the same time as a real one, resisting username enumeration
		// via response-time differences.
		_, _ = VerifyPassword(password, dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil || !ok {
		return nil, ErrInvalidCredentials
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE admin_users SET last_login_at = NOW() WHERE id = ?`, u.ID)
	return &u, nil
}

// dummyHash is a valid argon2id hash of an arbitrary password, used only to equalize
// timing for nonexistent usernames.
const dummyHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession returns the plaintext cookie value; only its hash is stored.
func (s *Store) CreateSession(ctx context.Context, userID int64, ip, userAgent string) (string, time.Time, error) {
	// Opportunistic cleanup, cheap enough to run on every login.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at < NOW()`)

	token, err := generateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt := time.Now().Add(sessionTTL)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO admin_sessions (admin_user_id, token_hash, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?)`,
		userID, hashToken(token), expiresAt, ip, userAgent)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *Store) VerifySession(ctx context.Context, token string) (*AdminUser, error) {
	if token == "" {
		return nil, ErrInvalidSession
	}
	var u AdminUser
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username FROM admin_sessions s
		JOIN admin_users u ON u.id = s.admin_user_id
		WHERE s.token_hash = ? AND s.expires_at > NOW()`, hashToken(token)).
		Scan(&u.ID, &u.Username)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) RevokeSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE token_hash = ?`, hashToken(token))
	return err
}
