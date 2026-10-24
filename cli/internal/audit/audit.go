// Package audit records administrative changes: who changed what, when, and from where.
// The dashboard, the CLI, the local API and the public API all write to it, and the
// dashboard's Audit log page reads it. Recording is best-effort: a failure to write an
// audit entry is logged but never fails the change it describes, because the change
// has already been made by then.
package audit

import (
	"context"
	"database/sql"
	"log"
	"strings"
	"time"
)

type Entry struct {
	ID     int64
	At     time.Time
	Actor  string
	Action string
	Target string
	Detail string
	IP     string
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Record writes one entry. action is a dotted verb such as "user.create"; target is
// the address or name it applied to.
func (s *Store) Record(ctx context.Context, actor, action, target, detail, ip string) {
	_, err := s.db.ExecContext(context.WithoutCancel(ctx), `
		INSERT INTO audit_log (actor, action, target, detail, ip) VALUES (?, ?, ?, ?, ?)`,
		clip(actor, 255), clip(action, 64), clip(target, 255), clip(detail, 1000), clip(ip, 64))
	if err != nil {
		log.Printf("audit: could not record %s %s by %s: %v", action, target, actor, err)
	}
}

type Filter struct {
	Query  string // substring of actor, action, target or detail
	Limit  int
	Offset int
}

// List returns one page of entries, newest first, and the total number of matches.
func (s *Store) List(ctx context.Context, f Filter) ([]Entry, int, error) {
	cond := "1=1"
	var args []any
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
		cond = "(actor LIKE ? OR action LIKE ? OR target LIKE ? OR detail LIKE ?)"
		args = []any{like, like, like, like}
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, at, actor, action, target, detail, ip FROM audit_log
		WHERE `+cond+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.At, &e.Actor, &e.Action, &e.Target, &e.Detail, &e.IP); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
