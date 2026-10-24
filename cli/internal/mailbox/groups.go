package mailbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Group posting policies: who may send mail to a group's address. Enforced by
// internal/policy at SMTP RCPT time, not by the expansion itself.
const (
	PostAnyone       = "anyone"       // anyone, including the whole internet
	PostOrganization = "organization" // any authenticated mailbox on this server
	PostMembers      = "members"      // the group's own members (and owners)
	PostOwners       = "owners"       // only the group's owners
)

var PostPolicies = []string{PostAnyone, PostOrganization, PostMembers, PostOwners}

func ValidPostPolicy(p string) bool {
	for _, v := range PostPolicies {
		if v == p {
			return true
		}
	}
	return false
}

const (
	RoleMember = "member"
	RoleOwner  = "owner"
)

// Group is a distribution list: mail to Email is delivered to every member. Postfix
// expands it through virtual_alias_maps (see mysql-virtual-alias-maps.cf.tmpl), which
// also expands members that are themselves groups.
type Group struct {
	ID            int64     `json:"-"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	PostPolicy    string    `json:"post_policy"`
	AllowExternal bool      `json:"allow_external"`
	MemberCount   int       `json:"member_count"`
	CreatedAt     time.Time `json:"created_at"`
}

type GroupMember struct {
	Address string `json:"address"`
	Role    string `json:"role"`
	// Kind is "user", "group", "alias", "external", or "missing" (a hosted-domain
	// address that no longer exists), derived at read time.
	Kind      string    `json:"kind"`
	Name      string    `json:"name,omitempty"`
	Suspended bool      `json:"suspended,omitempty"`
	CreatedAt time.Time `json:"added_at"`
}

// GroupSettings are the editable fields of a group.
type GroupSettings struct {
	Name          string
	Description   string
	PostPolicy    string
	AllowExternal bool
}

func (g GroupSettings) validate() error {
	if strings.TrimSpace(g.Name) == "" {
		return invalidf("group name is required")
	}
	if len(g.Name) > 255 {
		return invalidf("group name must be at most 255 characters")
	}
	if len(g.Description) > 1000 {
		return invalidf("description must be at most 1000 characters")
	}
	if !ValidPostPolicy(g.PostPolicy) {
		return invalidf("invalid posting policy %q (expected one of %s)", g.PostPolicy, strings.Join(PostPolicies, ", "))
	}
	return nil
}

// AddressKind reports what an address is on this server: "user", "group", "alias", or
// "" if it's none of them. Mailbox, group and alias addresses share one namespace.
func (s *Store) AddressKind(ctx context.Context, email string) (string, error) {
	var kind string
	err := s.db.QueryRowContext(ctx, `
		SELECT 'user' FROM virtual_users WHERE email = ?
		UNION ALL SELECT 'group' FROM mail_groups WHERE email = ?
		UNION ALL SELECT 'alias' FROM virtual_aliases WHERE source = ?
		LIMIT 1`, email, email, email).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return kind, err
}

func (s *Store) GroupCreate(ctx context.Context, email string, settings GroupSettings) error {
	email = strings.ToLower(strings.TrimSpace(email))
	_, domain, err := SplitEmail(email)
	if err != nil {
		return err
	}
	if err := settings.validate(); err != nil {
		return err
	}
	domID, err := s.domainID(ctx, domain)
	if err != nil {
		return err
	}
	kind, err := s.AddressKind(ctx, email)
	if err != nil {
		return err
	}
	if kind != "" {
		return fmt.Errorf("%w: %s is already in use by a %s", ErrExists, email, kind)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO mail_groups (domain_id, email, name, description, post_policy, allow_external)
		VALUES (?, ?, ?, ?, ?, ?)`,
		domID, email, strings.TrimSpace(settings.Name), strings.TrimSpace(settings.Description), settings.PostPolicy, settings.AllowExternal)
	if isDuplicateKey(err) {
		return fmt.Errorf("%w: group %q", ErrExists, email)
	}
	return err
}

func (s *Store) GroupUpdate(ctx context.Context, email string, settings GroupSettings) error {
	if err := settings.validate(); err != nil {
		return err
	}
	g, err := s.GroupGet(ctx, email)
	if err != nil {
		return err
	}
	if !settings.AllowExternal && g.AllowExternal {
		var external int
		members, err := s.GroupMembers(ctx, email)
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.Kind == "external" {
				external++
			}
		}
		if external > 0 {
			return invalidf("remove the group's %d external member(s) before disallowing external members", external)
		}
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE mail_groups SET name = ?, description = ?, post_policy = ?, allow_external = ? WHERE id = ?`,
		strings.TrimSpace(settings.Name), strings.TrimSpace(settings.Description), settings.PostPolicy, settings.AllowExternal, g.ID)
	return err
}

func (s *Store) GroupDelete(ctx context.Context, email string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mail_groups WHERE email = ?`, strings.ToLower(email))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: group %q", ErrNotFound, email)
	}
	// It may itself have been a member of other groups.
	_, err = s.db.ExecContext(ctx, `DELETE FROM mail_group_members WHERE member = ?`, strings.ToLower(email))
	return err
}

const groupSelect = `
	SELECT g.id, g.email, g.name, g.description, g.post_policy, g.allow_external, g.created_at,
	       (SELECT COUNT(*) FROM mail_group_members m WHERE m.group_id = g.id)
	FROM mail_groups g`

func scanGroup(sc interface{ Scan(...any) error }) (Group, error) {
	var g Group
	var external int
	err := sc.Scan(&g.ID, &g.Email, &g.Name, &g.Description, &g.PostPolicy, &external, &g.CreatedAt, &g.MemberCount)
	g.AllowExternal = external != 0
	return g, err
}

func (s *Store) GroupGet(ctx context.Context, email string) (*Group, error) {
	g, err := scanGroup(s.db.QueryRowContext(ctx, groupSelect+` WHERE g.email = ?`, strings.ToLower(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: group %q", ErrNotFound, email)
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// GroupList lists groups, optionally limited to one domain and/or filtered by a
// case-insensitive substring of the address or name.
func (s *Store) GroupList(ctx context.Context, domain, query string) ([]Group, error) {
	where := []string{"1=1"}
	var args []any
	if domain != "" {
		where = append(where, "g.domain_id = (SELECT id FROM virtual_domains WHERE name = ?)")
		args = append(args, domain)
	}
	if query = strings.TrimSpace(query); query != "" {
		where = append(where, "(g.email LIKE ? OR g.name LIKE ?)")
		like := "%" + escapeLike(query) + "%"
		args = append(args, like, like)
	}
	rows, err := s.db.QueryContext(ctx, groupSelect+` WHERE `+strings.Join(where, " AND ")+` ORDER BY g.email`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GroupMembers lists a group's members, owners first.
func (s *Store) GroupMembers(ctx context.Context, email string) ([]GroupMember, error) {
	g, err := s.GroupGet(ctx, email)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.member, m.role, m.created_at,
		       CASE WHEN u.email IS NOT NULL THEN 'user'
		            WHEN sg.email IS NOT NULL THEN 'group'
		            WHEN EXISTS (SELECT 1 FROM virtual_aliases a WHERE a.source = m.member) THEN 'alias'
		            WHEN d.id IS NOT NULL THEN 'missing'
		            ELSE 'external' END,
		       COALESCE(u.display_name, sg.name, ''), COALESCE(u.enabled, 1) = 0
		FROM mail_group_members m
		LEFT JOIN virtual_users u ON u.email = m.member
		LEFT JOIN mail_groups sg ON sg.email = m.member
		LEFT JOIN virtual_domains d ON d.name = SUBSTRING_INDEX(m.member, '@', -1)
		WHERE m.group_id = ?
		ORDER BY m.role = 'owner' DESC, m.member`, g.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []GroupMember{}
	for rows.Next() {
		var m GroupMember
		if err := rows.Scan(&m.Address, &m.Role, &m.CreatedAt, &m.Kind, &m.Name, &m.Suspended); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemberResult is the outcome of adding one address in GroupAddMembers.
type MemberResult struct {
	Address string `json:"address"`
	Added   bool   `json:"added"`
	Error   string `json:"error,omitempty"`
}

// GroupAddMembers adds addresses to a group with the given role, reporting per address
// rather than failing the whole batch: pasting a class list with one typo shouldn't lose
// the other 39 additions. Re-adding an existing member updates their role.
func (s *Store) GroupAddMembers(ctx context.Context, email string, addresses []string, role string) ([]MemberResult, error) {
	if role != RoleMember && role != RoleOwner {
		return nil, invalidf("invalid role %q (expected member or owner)", role)
	}
	g, err := s.GroupGet(ctx, email)
	if err != nil {
		return nil, err
	}
	hosted, err := s.hostedDomains(ctx)
	if err != nil {
		return nil, err
	}

	results := make([]MemberResult, 0, len(addresses))
	for _, addr := range addresses {
		addr = strings.ToLower(strings.TrimSpace(addr))
		if addr == "" {
			continue
		}
		res := MemberResult{Address: addr}
		_, domain, verr := SplitEmail(addr)
		switch {
		case verr != nil:
			res.Error = "not a valid email address"
		case addr == g.Email:
			res.Error = "a group can't be a member of itself"
		case hosted[domain]:
			kind, kerr := s.AddressKind(ctx, addr)
			if kerr != nil {
				return nil, kerr
			}
			if kind == "" {
				res.Error = "no mailbox, group or alias with this address exists on this server"
			}
		case !g.AllowExternal:
			res.Error = "external addresses aren't allowed in this group (change it in the group's settings)"
		}
		if res.Error == "" {
			_, err := s.db.ExecContext(ctx, `
				INSERT INTO mail_group_members (group_id, member, role) VALUES (?, ?, ?)
				ON DUPLICATE KEY UPDATE role = VALUES(role)`, g.ID, addr, role)
			if err != nil {
				return nil, err
			}
			res.Added = true
		}
		results = append(results, res)
	}
	return results, nil
}

func (s *Store) GroupRemoveMember(ctx context.Context, email, member string) error {
	g, err := s.GroupGet(ctx, email)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM mail_group_members WHERE group_id = ? AND member = ?`, g.ID, strings.ToLower(member))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: %s is not a member of %s", ErrNotFound, member, email)
	}
	return nil
}

func (s *Store) GroupSetMemberRole(ctx context.Context, email, member, role string) error {
	if role != RoleMember && role != RoleOwner {
		return invalidf("invalid role %q (expected member or owner)", role)
	}
	g, err := s.GroupGet(ctx, email)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE mail_group_members SET role = ? WHERE group_id = ? AND member = ?`, role, g.ID, strings.ToLower(member))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		_ = s.db.QueryRowContext(ctx, `SELECT 1 FROM mail_group_members WHERE group_id = ? AND member = ?`, g.ID, strings.ToLower(member)).Scan(&exists)
		if exists == 0 {
			return fmt.Errorf("%w: %s is not a member of %s", ErrNotFound, member, email)
		}
	}
	return nil
}

// GroupsOf lists the groups an address is a direct member of.
func (s *Store) GroupsOf(ctx context.Context, address string) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, groupSelect+`
		JOIN mail_group_members gm ON gm.group_id = g.id
		WHERE gm.member = ? ORDER BY g.email`, strings.ToLower(address))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) hostedDomains(ctx context.Context) (map[string]bool, error) {
	domains, err := s.DomainList(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(domains))
	for _, d := range domains {
		out[strings.ToLower(d.Name)] = true
	}
	return out, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
