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
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	VmailUID  = 5000
	VmailGID  = 5000
	VmailHome = "/var/mail/vhosts"
)

var (
	domainRe = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,}$`)
	emailRe  = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

func ValidateDomain(d string) error {
	if !domainRe.MatchString(d) {
		return fmt.Errorf("invalid domain: %s", d)
	}
	return nil
}

func ValidateEmail(e string) error {
	if !emailRe.MatchString(e) {
		return fmt.Errorf("invalid email address: %s", e)
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
	Email      string `json:"email"`
	Enabled    bool   `json:"enabled"`
	QuotaBytes int64  `json:"quota_bytes"`
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

var ErrNotFound = errors.New("not found")

func (s *Store) DomainAdd(ctx context.Context, name string) error {
	if err := ValidateDomain(name); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO virtual_domains (name) VALUES (?)`, name)
	return err
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

func hashPassword(password string) (string, error) {
	cmd := exec.Command("doveadm", "pw", "-s", "SHA512-CRYPT", "-p", password)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("doveadm pw: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func mailboxDir(domain, local string) string {
	return filepath.Join(VmailHome, domain, local)
}

func (s *Store) MailboxAdd(ctx context.Context, email, password string, quotaBytes int64) error {
	local, domain, err := SplitEmail(email)
	if err != nil {
		return err
	}
	domID, err := s.domainID(ctx, domain)
	if err != nil {
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
			SELECT vu.email, vu.enabled, vu.quota_bytes FROM virtual_users vu
			JOIN virtual_domains vd ON vd.id = vu.domain_id
			WHERE vd.name = ? ORDER BY vu.email`, domain)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT email, enabled, quota_bytes FROM virtual_users ORDER BY email`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mailbox
	for rows.Next() {
		var m Mailbox
		var enabled int
		if err := rows.Scan(&m.Email, &enabled, &m.QuotaBytes); err != nil {
			return nil, err
		}
		m.Enabled = enabled != 0
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) MailboxRemove(ctx context.Context, email string) error {
	local, domain, err := SplitEmail(email)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM virtual_users WHERE email = ?`, email); err != nil {
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
			WHERE vd.name = ? ORDER BY va.source`, domain)
	} else {
		rows, err = s.db.QueryContext(ctx, `SELECT source, destination FROM virtual_aliases ORDER BY source`)
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
