package mailbox

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Forward is a mailbox's forwarding rule. Postfix applies it through
// virtual_alias_maps; with KeepCopy the mailbox also keeps receiving the mail.
type Forward struct {
	Destination string `json:"destination"`
	KeepCopy    bool   `json:"keep_copy"`
}

func (s *Store) ForwardGet(ctx context.Context, email string) (*Forward, error) {
	var f Forward
	var keep int
	err := s.db.QueryRowContext(ctx, `SELECT destination, keep_copy FROM mailbox_forwards WHERE email = ?`, email).Scan(&f.Destination, &keep)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.KeepCopy = keep != 0
	return &f, nil
}

// ForwardSet forwards a mailbox's incoming mail to destination, replacing any
// existing rule.
func (s *Store) ForwardSet(ctx context.Context, email, destination string, keepCopy bool) error {
	destination = strings.ToLower(strings.TrimSpace(destination))
	if err := ValidateEmail(destination); err != nil {
		return invalidf("invalid forwarding address: %s", destination)
	}
	if strings.EqualFold(destination, email) {
		return invalidf("a mailbox can't forward to itself")
	}
	if _, err := s.MailboxGet(ctx, email); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mailbox_forwards (email, destination, keep_copy) VALUES (?, ?, ?)
		ON DUPLICATE KEY UPDATE destination = VALUES(destination), keep_copy = VALUES(keep_copy)`,
		email, destination, keepCopy)
	return err
}

func (s *Store) ForwardClear(ctx context.Context, email string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM mailbox_forwards WHERE email = ?`, email)
	return err
}

// AliasesTo lists the alias addresses that deliver to destination, i.e. a user's
// alternate addresses.
func (s *Store) AliasesTo(ctx context.Context, destination string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source FROM virtual_aliases WHERE destination = ? AND source NOT LIKE '@%' ORDER BY source`, destination)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// CatchAllGet returns the address that receives mail for nonexistent addresses at
// domain, or "" if there is none.
func (s *Store) CatchAllGet(ctx context.Context, domain string) (string, error) {
	var dest string
	err := s.db.QueryRowContext(ctx, `SELECT destination FROM virtual_aliases WHERE source = ? LIMIT 1`, "@"+domain).Scan(&dest)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return dest, err
}

// CatchAllSet routes mail for any nonexistent address at domain to destination. It is
// stored as Postfix's native "@domain" alias. Existing mailboxes are unaffected because
// the alias map always maps a mailbox to itself first.
func (s *Store) CatchAllSet(ctx context.Context, domain, destination string) error {
	domID, err := s.domainID(ctx, domain)
	if err != nil {
		return err
	}
	if destination == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM virtual_aliases WHERE source = ?`, "@"+domain)
		return err
	}
	destination = strings.ToLower(strings.TrimSpace(destination))
	if err := ValidateEmail(destination); err != nil {
		return invalidf("invalid catch-all address: %s", destination)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM virtual_aliases WHERE source = ?`, "@"+domain); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO virtual_aliases (domain_id, source, destination) VALUES (?, ?, ?)`, domID, "@"+domain, destination); err != nil {
		return err
	}
	return tx.Commit()
}

// UsageBytes returns how much mail a mailbox currently stores, from Dovecot's
// Maildir++ quota file (Maildir/maildirsize: a header line, then "<bytes> <count>"
// deltas). Reading the file directly avoids running doveadm once per mailbox when
// listing hundreds of them. A mailbox that has never received mail has no file yet,
// which counts as 0.
func UsageBytes(email string) int64 {
	local, domain, err := SplitEmail(email)
	if err != nil {
		return 0
	}
	f, err := os.Open(filepath.Join(mailboxDir(domain, local), "Maildir", "maildirsize"))
	if err != nil {
		return 0
	}
	defer f.Close()
	var total int64
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first {
			first = false
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			total += n
		}
	}
	if total < 0 {
		return 0
	}
	return total
}

// MailboxFilter narrows MailboxSearch.
type MailboxFilter struct {
	Domain string
	Query  string // substring of the address or display name
	Status string // "", "active" or "suspended"
	Limit  int
	Offset int
}

// MailboxSearch returns one page of mailboxes matching f, and the total number of
// matches.
func (s *Store) MailboxSearch(ctx context.Context, f MailboxFilter) ([]Mailbox, int, error) {
	where := []string{"1=1"}
	var args []any
	if f.Domain != "" {
		where = append(where, "vu.domain_id = (SELECT id FROM virtual_domains WHERE name = ?)")
		args = append(args, f.Domain)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + escapeLike(q) + "%"
		where = append(where, "(vu.email LIKE ? OR vu.display_name LIKE ?)")
		args = append(args, like, like)
	}
	switch f.Status {
	case "active":
		where = append(where, "vu.enabled = 1")
	case "suspended":
		where = append(where, "vu.enabled = 0")
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM virtual_users vu WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT vu.email, vu.display_name, vu.enabled, vu.quota_bytes, vu.created_at FROM virtual_users vu
		WHERE `+cond+` ORDER BY vu.email LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Mailbox{}
	for rows.Next() {
		var m Mailbox
		var enabled int
		if err := rows.Scan(&m.Email, &m.DisplayName, &enabled, &m.QuotaBytes, &m.CreatedAt); err != nil {
			return nil, 0, err
		}
		m.Enabled = enabled != 0
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// DomainStats are per-domain object counts, for the dashboard.
type DomainStats struct {
	Users     int
	Suspended int
	Groups    int
	Aliases   int
}

func (s *Store) DomainStatsAll(ctx context.Context) (map[string]DomainStats, error) {
	out := map[string]DomainStats{}
	rows, err := s.db.QueryContext(ctx, `
		SELECT d.name,
		  (SELECT COUNT(*) FROM virtual_users u WHERE u.domain_id = d.id),
		  (SELECT COUNT(*) FROM virtual_users u WHERE u.domain_id = d.id AND u.enabled = 0),
		  (SELECT COUNT(*) FROM mail_groups g WHERE g.domain_id = d.id),
		  (SELECT COUNT(*) FROM virtual_aliases a WHERE a.domain_id = d.id AND a.source NOT LIKE '@%')
		FROM virtual_domains d`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var st DomainStats
		if err := rows.Scan(&name, &st.Users, &st.Suspended, &st.Groups, &st.Aliases); err != nil {
			return nil, err
		}
		out[name] = st
	}
	return out, rows.Err()
}
