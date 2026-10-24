// Package mailbox implements the domain/mailbox/alias business logic shared by the
// patrabahok CLI and the patrabahokd API daemon. All database access here is via
// parameterized queries — no string-built SQL.
package mailbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
)

const (
	VmailUID  = 5000
	VmailGID  = 5000
	VmailHome = "/var/mail/vhosts"
)

var (
	domainRe = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	// The local part must be an RFC 5322 dot-atom: no "/", no leading, trailing or
	// doubled dots. That matters beyond style, because a mailbox's local part becomes
	// a directory name under VmailHome: anything looser (e.g. "../../etc") would let
	// create and delete reach paths outside it as root.
	emailRe = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+=?^_{|}~-]+(\.[A-Za-z0-9!#$%&'*+=?^_{|}~-]+)*@([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,}$`)
)

// ValidationError is an error caused by the caller's input (as opposed to a database
// or system failure), safe to show to whoever supplied it.
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func invalidf(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// IsValidation reports whether err (or anything it wraps) is a ValidationError.
func IsValidation(err error) bool {
	var v *ValidationError
	return errors.As(err, &v)
}

func ValidateDomain(d string) error {
	if !domainRe.MatchString(d) {
		return invalidf("invalid domain: %s", d)
	}
	return nil
}

func ValidateEmail(e string) error {
	local, _, _ := strings.Cut(e, "@")
	if len(e) > 254 || len(local) > 64 || !emailRe.MatchString(e) {
		return invalidf("invalid email address: %s", e)
	}
	return nil
}

func SplitEmail(e string) (local, domain string, err error) {
	if err = ValidateEmail(e); err != nil {
		return "", "", err
	}
	i := strings.LastIndex(e, "@")
	return e[:i], e[i+1:], nil
}

// ParseQuota parses a human quota string like "500M" or "1G" into bytes.
func ParseQuota(q string) (int64, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		q = "1G"
	}
	i := 0
	for i < len(q) && q[i] >= '0' && q[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("invalid quota: %s (expected e.g. 500M, 1G)", q)
	}
	num, err := strconv.ParseInt(q[:i], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid quota: %s", q)
	}
	unit := strings.ToUpper(strings.TrimSpace(q[i:]))
	switch unit {
	case "", "B":
		return num, nil
	case "K", "KB":
		return num * 1024, nil
	case "M", "MB":
		return num * 1024 * 1024, nil
	case "G", "GB":
		return num * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("invalid quota unit: %s", unit)
	}
}

type Domain struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Mailbox struct {
	Email       string    `json:"email"`
	DisplayName string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	QuotaBytes  int64     `json:"quota_bytes"`
	CreatedAt   time.Time `json:"-"`
}

// Password length limits shared by the dashboard and the public API.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

func ValidatePassword(p string) error {
	n := utf8.RuneCountInString(p)
	if n < MinPasswordLength || n > MaxPasswordLength {
		return invalidf("password must be between %d and %d characters", MinPasswordLength, MaxPasswordLength)
	}
	return nil
}

type Alias struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

var (
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

// isDuplicateKey reports whether err is MariaDB's duplicate-key error (1062).
func isDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func (s *Store) DomainAdd(ctx context.Context, name string) error {
	if err := ValidateDomain(name); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO virtual_domains (name) VALUES (?)`, name); err != nil {
		return err
	}
	if err := ensureDKIMAndDNSRecords(name); err != nil {
		return fmt.Errorf("domain added, but DKIM key/DNS record generation failed: %w", err)
	}
	return nil
}

func (s *Store) DomainList(ctx context.Context) ([]Domain, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name FROM virtual_domains ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Domain
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.ID, &d.Name); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) domainID(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM virtual_domains WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: domain %q is not registered (run: domain add %s)", ErrNotFound, name, name)
	}
	return id, err
}

func (s *Store) DomainRemove(ctx context.Context, name string) error {
	if err := ValidateDomain(name); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM virtual_domains WHERE name = ?`, name)
	return err
}

// hashPassword returns a Dovecot {SHA512-CRYPT} hash (see sha512crypt.go).
func hashPassword(password string) (string, error) {
	salt, err := randomSalt()
	if err != nil {
		return "", err
	}
	return "{SHA512-CRYPT}" + sha512Crypt(password, salt, 5000), nil
}

func mailboxDir(domain, local string) string {
	return filepath.Join(VmailHome, domain, local)
}

// MailboxAdd creates a mailbox, or — if it already exists — resets its password and
// quota (the CLI/admin semantics: "make this mailbox exist with these settings").
func (s *Store) MailboxAdd(ctx context.Context, email, password string, quotaBytes int64) error {
	local, domain, err := SplitEmail(email)
	if err != nil {
		return err
	}
	domID, err := s.domainID(ctx, domain)
	if err != nil {
		return err
	}
	if err := s.notAGroup(ctx, email); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	maildir := domain + "/" + local

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO virtual_users (domain_id, email, password, maildir, quota_bytes)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE password = VALUES(password), quota_bytes = VALUES(quota_bytes)`,
		domID, email, hash, maildir, quotaBytes)
	if err != nil {
		return err
	}
	return createMaildir(domain, local)
}

// MailboxCreate creates a new mailbox and fails with ErrExists if it already exists —
// unlike MailboxAdd, it never touches an existing mailbox's password. Used by the public
// API, where a repeated or mistaken "create" must not silently reset someone's password.
func (s *Store) MailboxCreate(ctx context.Context, email, password, displayName string, quotaBytes int64) error {
	local, domain, err := SplitEmail(email)
	if err != nil {
		return err
	}
	domID, err := s.domainID(ctx, domain)
	if err != nil {
		return err
	}
	if kind, err := s.AddressKind(ctx, email); err != nil {
		return err
	} else if kind == "group" || kind == "user" {
		return fmt.Errorf("%w: %s is already in use by a %s", ErrExists, email, kind)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO virtual_users (domain_id, email, display_name, password, maildir, quota_bytes)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domID, email, displayName, hash, domain+"/"+local, quotaBytes)
	if isDuplicateKey(err) {
		return fmt.Errorf("%w: mailbox %q", ErrExists, email)
	}
	if err != nil {
		return err
	}
	return createMaildir(domain, local)
}

func (s *Store) notAGroup(ctx context.Context, email string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mail_groups WHERE email = ?`, email).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("%w: %s is a group address", ErrExists, email)
	}
	return nil
}

func createMaildir(domain, local string) error {
	dir := mailboxDir(domain, local)
	for _, sub := range []string{"Maildir/cur", "Maildir/new", "Maildir/tmp", "sieve"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return fmt.Errorf("create %s: %w", sub, err)
		}
	}
	if err := chownRecursive(filepath.Join(VmailHome, domain), VmailUID, VmailGID); err != nil {
		return fmt.Errorf("chown mailbox tree: %w", err)
	}
	return nil
}

func chownRecursive(root string, uid, gid int) error {
	return filepath.Walk(root, func(path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return os.Chown(path, uid, gid)
	})
}

func (s *Store) MailboxList(ctx context.Context, domain string) ([]Mailbox, error) {
	var rows *sql.Rows
	var err error
	if domain != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT vu.email, vu.display_name, vu.enabled, vu.quota_bytes FROM virtual_users vu
			JOIN virtual_domains vd ON vd.id = vu.domain_id
			WHERE vd.name = ? ORDER BY vu.email`, domain)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT email, display_name, enabled, quota_bytes FROM virtual_users ORDER BY email`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mailbox
	for rows.Next() {
		var m Mailbox
		var enabled int
		if err := rows.Scan(&m.Email, &m.DisplayName, &enabled, &m.QuotaBytes); err != nil {
			return nil, err
		}
		m.Enabled = enabled != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MailboxGet(ctx context.Context, email string) (*Mailbox, error) {
	if err := ValidateEmail(email); err != nil {
		return nil, err
	}
	var m Mailbox
	var enabled int
	err := s.db.QueryRowContext(ctx, `
		SELECT email, display_name, enabled, quota_bytes, created_at FROM virtual_users WHERE email = ?`, email,
	).Scan(&m.Email, &m.DisplayName, &enabled, &m.QuotaBytes, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: mailbox %q", ErrNotFound, email)
	}
	if err != nil {
		return nil, err
	}
	m.Enabled = enabled != 0
	return &m, nil
}

func (s *Store) MailboxRemove(ctx context.Context, email string) error {
	local, domain, err := SplitEmail(email)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM virtual_users WHERE email = ?`, email); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mail_group_members WHERE member = ?`, email); err != nil {
		return err
	}
	return os.RemoveAll(mailboxDir(domain, local))
}

func (s *Store) MailboxPasswd(ctx context.Context, email, password string) error {
	if err := ValidateEmail(email); err != nil {
		return err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE virtual_users SET password = ? WHERE email = ?`, hash, email)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("%w: mailbox %q", ErrNotFound, email)
	}
	return nil
}

// MailboxSetQuota changes an existing mailbox's quota. Dovecot reads virtual_users.
// quota_bytes on every login/delivery via userdb's quota_rule extra field (see
// templates/dovecot/dovecot-sql.conf.ext.tmpl) — no restart or cache to invalidate,
// this takes effect on the mailbox's very next IMAP session or delivery.
func (s *Store) MailboxSetQuota(ctx context.Context, email string, quotaBytes int64) error {
	if err := ValidateEmail(email); err != nil {
		return err
	}
	if quotaBytes <= 0 {
		return invalidf("quota must be greater than zero")
	}
	return s.updateMailbox(ctx, email, `UPDATE virtual_users SET quota_bytes = ? WHERE email = ?`, quotaBytes)
}

// MailboxSetName changes a mailbox's display name. It's informational (shown in the
// dashboard/API), not used by Postfix or Dovecot.
func (s *Store) MailboxSetName(ctx context.Context, email, displayName string) error {
	if err := ValidateEmail(email); err != nil {
		return err
	}
	if len(displayName) > 255 {
		return invalidf("name must be at most 255 characters")
	}
	return s.updateMailbox(ctx, email, `UPDATE virtual_users SET display_name = ? WHERE email = ?`, displayName)
}

// MailboxSetEnabled enables or disables a mailbox. Disabled mailboxes can't log in and
// don't receive mail (every Postfix/Dovecot lookup filters on enabled = 1), but their
// stored mail is kept.
func (s *Store) MailboxSetEnabled(ctx context.Context, email string, enabled bool) error {
	if err := ValidateEmail(email); err != nil {
		return err
	}
	return s.updateMailbox(ctx, email, `UPDATE virtual_users SET enabled = ? WHERE email = ?`, enabled)
}

// updateMailbox runs a single-value UPDATE on one mailbox. MariaDB reports zero affected
// rows when the new value equals the old one, so a miss is double-checked for existence
// rather than reported as not-found outright.
func (s *Store) updateMailbox(ctx context.Context, email, query string, value any) error {
	res, err := s.db.ExecContext(ctx, query, value, email)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.MailboxGet(ctx, email); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AliasAdd(ctx context.Context, source, destination string) error {
	if err := ValidateEmail(source); err != nil {
		return fmt.Errorf("invalid alias address: %w", err)
	}
	if err := ValidateEmail(destination); err != nil {
		return fmt.Errorf("invalid target address: %w", err)
	}
	_, domain, err := SplitEmail(source)
	if err != nil {
		return err
	}
	domID, err := s.domainID(ctx, domain)
	if err != nil {
		return err
	}
	if err := s.notAGroup(ctx, source); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT IGNORE INTO virtual_aliases (domain_id, source, destination) VALUES (?, ?, ?)`,
		domID, source, destination)
	return err
}

func (s *Store) AliasList(ctx context.Context, domain string) ([]Alias, error) {
	var rows *sql.Rows
	var err error
	if domain != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT va.source, va.destination FROM virtual_aliases va
			JOIN virtual_domains vd ON vd.id = va.domain_id
			WHERE vd.name = ? AND va.source NOT LIKE '@%' ORDER BY va.source`, domain)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT source, destination FROM virtual_aliases WHERE source NOT LIKE '@%' ORDER BY source`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alias
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.Source, &a.Destination); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) AliasRemove(ctx context.Context, source, destination string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM virtual_aliases WHERE source = ? AND destination = ?`, source, destination)
	return err
}
