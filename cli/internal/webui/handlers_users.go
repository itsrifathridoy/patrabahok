package webui

import (
	"bytes"
	"crypto/rand"
	"encoding/csv"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

const usersPageSize = 25

// quotaChoices are the quota presets offered in forms.
var quotaChoices = []string{"500M", "1G", "2G", "5G", "10G", "25G", "50G"}

type UserRow struct {
	mailbox.Mailbox
	UsedBytes int64
}

type UsersPageData struct {
	Base
	Users        []UserRow
	Domains      []mailbox.Domain
	Pager        Pager
	Query        string
	Domain       string
	Status       string
	TotalUsers   int
	ActiveUsers  int
	Suspended    int
	QuotaChoices []string
}

func (s *Server) handleUsersPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	data := UsersPageData{
		Base:         s.base(w, r, "Users", "users", Crumb{Label: "Users"}),
		Query:        strings.TrimSpace(q.Get("q")),
		Domain:       q.Get("domain"),
		Status:       q.Get("status"),
		QuotaChoices: quotaChoices,
	}
	var err error
	if data.Domains, err = s.store.DomainList(ctx); err != nil {
		failErr(w, r, err)
		return
	}
	stats, err := s.store.DomainStatsAll(ctx)
	if err != nil {
		failErr(w, r, err)
		return
	}
	for _, st := range stats {
		data.TotalUsers += st.Users
		data.Suspended += st.Suspended
	}
	data.ActiveUsers = data.TotalUsers - data.Suspended

	pager := newPager(r, usersPageSize, 0)
	list, total, err := s.store.MailboxSearch(ctx, mailbox.MailboxFilter{
		Domain: data.Domain, Query: data.Query, Status: data.Status,
		Limit: usersPageSize, Offset: pager.Offset(),
	})
	if err != nil {
		failErr(w, r, err)
		return
	}
	pager.Total = total
	data.Pager = pager
	for _, m := range list {
		data.Users = append(data.Users, UserRow{Mailbox: m, UsedBytes: mailbox.UsageBytes(m.Email)})
	}
	renderPage(w, "users", data)
}

func userURL(email string) string { return "/users/" + url.PathEscape(email) }

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, r, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	email := strings.ToLower(formValue(r, "local") + "@" + formValue(r, "domain"))
	password := r.FormValue("password")
	if err := mailbox.ValidatePassword(password); err != nil {
		failErr(w, r, err)
		return
	}
	quota, err := mailbox.ParseQuota(formValue(r, "quota"))
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	name := formValue(r, "name")
	if err := s.store.MailboxCreate(r.Context(), email, password, name, quota); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "user.create", email, "quota="+humanizeBytes(quota))
	done(w, r, userURL(email), "User "+email+" created.")
}

type UserPageData struct {
	Base
	User         mailbox.Mailbox
	UsedBytes    int64
	Forward      *mailbox.Forward
	Aliases      []string
	Groups       []mailbox.Group
	OtherGroups  []mailbox.Group
	Domains      []mailbox.Domain
	QuotaChoices []string
	Activity     []AuditView
}

func (s *Server) handleUserPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := strings.ToLower(r.PathValue("email"))
	u, err := s.store.MailboxGet(ctx, email)
	if err != nil {
		if isSentinel(err, mailbox.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		failErr(w, r, err)
		return
	}
	title := u.DisplayName
	if title == "" {
		title = u.Email
	}
	data := UserPageData{
		Base:         s.base(w, r, title, "users", Crumb{Label: "Users", Href: "/users"}, Crumb{Label: u.Email}),
		User:         *u,
		UsedBytes:    mailbox.UsageBytes(u.Email),
		QuotaChoices: quotaChoices,
	}
	if data.Forward, err = s.store.ForwardGet(ctx, email); err != nil {
		failErr(w, r, err)
		return
	}
	if data.Aliases, err = s.store.AliasesTo(ctx, email); err != nil {
		failErr(w, r, err)
		return
	}
	if data.Groups, err = s.store.GroupsOf(ctx, email); err != nil {
		failErr(w, r, err)
		return
	}
	all, err := s.store.GroupList(ctx, "", "")
	if err != nil {
		failErr(w, r, err)
		return
	}
	member := map[string]bool{}
	for _, g := range data.Groups {
		member[g.Email] = true
	}
	for _, g := range all {
		if !member[g.Email] {
			data.OtherGroups = append(data.OtherGroups, g)
		}
	}
	if data.Domains, err = s.store.DomainList(ctx); err != nil {
		failErr(w, r, err)
		return
	}
	data.Activity = s.recentActivity(r, email, 8)
	renderPage(w, "user", data)
}

func (s *Server) handleUserProfile(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	name := formValue(r, "name")
	if err := s.store.MailboxSetName(r.Context(), email, name); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "user.rename", email, name)
	done(w, r, userURL(email), "Profile saved.")
}

func (s *Server) handleUserPassword(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	password := r.FormValue("password")
	if err := mailbox.ValidatePassword(password); err != nil {
		failErr(w, r, err)
		return
	}
	if err := s.store.MailboxPasswd(r.Context(), email, password); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "user.password.reset", email, "")
	done(w, r, userURL(email), "Password changed. It takes effect at the next sign-in.")
}

func (s *Server) handleUserQuota(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	quota, err := mailbox.ParseQuota(formValue(r, "quota"))
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.MailboxSetQuota(r.Context(), email, quota); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "user.quota", email, humanizeBytes(quota))
	done(w, r, userURL(email), "Storage limit set to "+humanizeBytes(quota)+".")
}

func (s *Server) handleUserStatus(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	enable := r.FormValue("enabled") == "1"
	if err := s.store.MailboxSetEnabled(r.Context(), email, enable); err != nil {
		failErr(w, r, err)
		return
	}
	if enable {
		s.record(r, "user.restore", email, "")
		done(w, r, userURL(email), email+" restored. They can sign in and receive mail again.")
		return
	}
	s.record(r, "user.suspend", email, "")
	done(w, r, userURL(email), email+" suspended. Their stored mail is kept.")
}

func (s *Server) handleUserForward(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	dest := formValue(r, "destination")
	keep := r.FormValue("keep_copy") == "1"
	if err := s.store.ForwardSet(r.Context(), email, dest, keep); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "user.forward.set", email, dest)
	done(w, r, userURL(email), "Mail for "+email+" now forwards to "+strings.ToLower(dest)+".")
}

func (s *Server) handleUserForwardClear(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if err := s.store.ForwardClear(r.Context(), email); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "user.forward.clear", email, "")
	done(w, r, userURL(email), "Forwarding turned off.")
}

func (s *Server) handleUserAliasAdd(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	alias := strings.ToLower(formValue(r, "local") + "@" + formValue(r, "domain"))
	if kind, err := s.store.AddressKind(r.Context(), alias); err != nil {
		failErr(w, r, err)
		return
	} else if kind == "user" || kind == "group" {
		fail(w, r, http.StatusConflict, alias+" is already a "+kind+" address.")
		return
	}
	if err := s.store.AliasAdd(r.Context(), alias, email); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "alias.add", alias, "→ "+email)
	done(w, r, userURL(email), alias+" now delivers to "+email+".")
}

func (s *Server) handleUserAliasRemove(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	alias := r.PathValue("alias")
	if err := s.store.AliasRemove(r.Context(), alias, email); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "alias.remove", alias, "→ "+email)
	done(w, r, userURL(email), "Removed "+alias+".")
}

func (s *Server) handleUserGroupAdd(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	group := formValue(r, "group")
	role := formValue(r, "role")
	if role == "" {
		role = mailbox.RoleMember
	}
	results, err := s.store.GroupAddMembers(r.Context(), group, []string{email}, role)
	if err != nil {
		failErr(w, r, err)
		return
	}
	if len(results) == 1 && !results[0].Added {
		fail(w, r, http.StatusBadRequest, results[0].Error)
		return
	}
	s.record(r, "group.members.add", group, email+" ("+role+")")
	done(w, r, userURL(email), "Added to "+group+".")
}

func (s *Server) handleUserGroupRemove(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	group := r.PathValue("group")
	if err := s.store.GroupRemoveMember(r.Context(), group, email); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "group.members.remove", group, email)
	done(w, r, userURL(email), "Removed from "+group+".")
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if _, err := s.store.MailboxGet(r.Context(), email); err != nil {
		failErr(w, r, err)
		return
	}
	if err := s.store.MailboxRemove(r.Context(), email); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "user.delete", email, "")
	done(w, r, "/users", "Deleted "+email+" and its stored mail.")
}

// --- bulk import ------------------------------------------------------------------

const maxImportRows = 1000

type ImportResult struct {
	Email    string
	Name     string
	Password string
	Created  bool
	Error    string
}

type UsersImportPageData struct {
	Base
	Domains      []mailbox.Domain
	QuotaChoices []string
	Results      []ImportResult
	CreatedCount int
	FailedCount  int
	// CredentialsCSV holds "email,name,password" rows for every created user, so the
	// admin can download them right away. It's only ever in this one response.
	CredentialsCSV string
}

func (s *Server) handleUsersImportPage(w http.ResponseWriter, r *http.Request) {
	domains, err := s.store.DomainList(r.Context())
	if err != nil {
		failErr(w, r, err)
		return
	}
	renderPage(w, "users_import", UsersImportPageData{
		Base:         s.base(w, r, "Import users", "users", Crumb{Label: "Users", Href: "/users"}, Crumb{Label: "Import"}),
		Domains:      domains,
		QuotaChoices: quotaChoices,
	})
}

// handleUsersImport creates users from CSV rows: "email-or-username[,name[,password[,quota]]]".
// A bare username uses the domain chosen in the form, a missing password is generated,
// and a missing quota uses the form's default. Rows are processed independently, so
// one bad row doesn't stop the rest.
func (s *Server) handleUsersImport(w http.ResponseWriter, r *http.Request) {
	// Hashing every password spawns doveadm, so a large import can outrun the server's
	// normal 15-second write timeout.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(10 * time.Minute))
	if err := r.ParseMultipartForm(4 << 20); err != nil && err != http.ErrNotMultipart {
		fail(w, r, http.StatusBadRequest, "Invalid upload.")
		return
	}
	defaultDomain := formValue(r, "domain")
	defaultQuota := formValue(r, "quota")

	raw := r.FormValue("csv")
	if file, _, err := r.FormFile("file"); err == nil {
		b, err := io.ReadAll(io.LimitReader(file, 4<<20))
		file.Close()
		if err != nil {
			fail(w, r, http.StatusBadRequest, "Could not read the uploaded file.")
			return
		}
		raw = string(b)
	}
	rows, err := parseImportCSV(raw)
	if err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if len(rows) == 0 {
		fail(w, r, http.StatusBadRequest, "No rows to import. Paste CSV or choose a file.")
		return
	}
	if len(rows) > maxImportRows {
		fail(w, r, http.StatusBadRequest, fmt.Sprintf("At most %d users per import (this file has %d).", maxImportRows, len(rows)))
		return
	}

	data := UsersImportPageData{
		Base:         s.base(w, r, "Import users", "users", Crumb{Label: "Users", Href: "/users"}, Crumb{Label: "Import"}),
		QuotaChoices: quotaChoices,
	}
	data.Domains, _ = s.store.DomainList(r.Context())

	var creds bytes.Buffer
	cw := csv.NewWriter(&creds)
	_ = cw.Write([]string{"email", "name", "password"})
	for _, row := range rows {
		res := ImportResult{Email: strings.ToLower(row[0])}
		if len(row) > 1 {
			res.Name = row[1]
		}
		if !strings.Contains(res.Email, "@") {
			if defaultDomain == "" {
				res.Error = "no domain: use a full address or pick a default domain"
				data.Results = append(data.Results, res)
				continue
			}
			res.Email += "@" + defaultDomain
		}
		if len(row) > 2 && row[2] != "" {
			res.Password = row[2]
		} else {
			res.Password = generatePassword()
		}
		quotaStr := defaultQuota
		if len(row) > 3 && row[3] != "" {
			quotaStr = row[3]
		}
		quota, qerr := mailbox.ParseQuota(quotaStr)
		switch {
		case qerr != nil:
			res.Error = qerr.Error()
		case mailbox.ValidatePassword(res.Password) != nil:
			res.Error = mailbox.ValidatePassword(res.Password).Error()
		default:
			if err := s.store.MailboxCreate(r.Context(), res.Email, res.Password, res.Name, quota); err != nil {
				if mailbox.IsValidation(err) || isSentinel(err, mailbox.ErrExists) || isSentinel(err, mailbox.ErrNotFound) {
					res.Error = err.Error()
				} else {
					res.Error = "server error (see the server log)"
				}
			} else {
				res.Created = true
				_ = cw.Write([]string{res.Email, res.Name, res.Password})
			}
		}
		if res.Created {
			data.CreatedCount++
		} else {
			data.FailedCount++
			res.Password = ""
		}
		data.Results = append(data.Results, res)
	}
	cw.Flush()
	data.CredentialsCSV = creds.String()
	s.record(r, "user.import", "", fmt.Sprintf("%d created, %d failed", data.CreatedCount, data.FailedCount))
	renderPage(w, "users_import", data)
}

// parseImportCSV reads rows, skipping blank lines, "#" comments and a header row.
func parseImportCSV(raw string) ([][]string, error) {
	cr := csv.NewReader(strings.NewReader(raw))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	cr.Comment = '#'
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not parse CSV: %v", err)
	}
	var out [][]string
	for i, rec := range records {
		for j := range rec {
			rec[j] = strings.TrimSpace(rec[j])
		}
		if len(rec) == 0 || rec[0] == "" {
			continue
		}
		if i == 0 {
			h := strings.ToLower(rec[0])
			if h == "email" || h == "username" || h == "user" || h == "address" {
				continue
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

// generatePassword returns a random 14-character password without look-alike
// characters (no 0/O, 1/l/I), so it can be read out or typed from a printout.
func generatePassword() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	b := make([]byte, 14)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			panic(err)
		}
		b[i] = alphabet[n.Int64()]
	}
	return string(b)
}
