package webui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/web"
)

type Crumb struct {
	Label string
	Href  string
}

// Base is embedded in every page's data: what the layout needs.
type Base struct {
	Title    string
	Active   string
	Username string
	Crumbs   []Crumb
	Flash    *Flash
}

type Flash struct {
	Kind    string `json:"kind"` // "success" or "error"
	Message string `json:"message"`
}

var pageFiles = map[string]string{
	"overview":     "templates/overview.html",
	"search":       "templates/search.html",
	"users":        "templates/users.html",
	"user":         "templates/user.html",
	"users_import": "templates/users_import.html",
	"groups":       "templates/groups.html",
	"group":        "templates/group.html",
	"aliases":      "templates/aliases.html",
	"domains":      "templates/domains.html",
	"domain":       "templates/domain.html",
	"dkim":         "templates/dkim.html",
	"queue":        "templates/queue.html",
	"bans":         "templates/bans.html",
	"services":     "templates/services.html",
	"logs":         "templates/logs.html",
	"diagnostics":  "templates/diagnostics.html",
	"tokens":       "templates/tokens.html",
	"admins":       "templates/admins.html",
	"audit":        "templates/audit.html",
	"settings":     "templates/settings.html",
}

var pageTemplates = map[string]*template.Template{}
var loginTemplate *template.Template
var iconTemplates *template.Template

// mailHostFunc backs the {{mailHost}} template function (used by the sidebar's server
// status card) — a template func rather than a Base field so every page's handler
// doesn't need to remember to populate it.
func mailHostFunc() string {
	host, _, _ := stateConfigStrings()
	return host
}

// iconFunc renders an icon by name ({{icon "users"}}), so templates can pick icons
// dynamically, which {{template "icon-..."}} (a constant name) can't do.
func iconFunc(name string) template.HTML {
	var b bytes.Buffer
	if err := iconTemplates.ExecuteTemplate(&b, "icon-"+name, nil); err != nil {
		return ""
	}
	return template.HTML(b.String())
}

func initials(s string) string {
	s = strings.TrimSpace(s)
	if local, _, ok := strings.Cut(s, "@"); ok {
		s = local
	}
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == '.' || r == '_' || r == '-' })
	switch {
	case len(fields) >= 2:
		return firstRune(fields[0]) + firstRune(fields[1])
	case len(fields) == 1:
		r := []rune(fields[0])
		if len(r) >= 2 {
			return string(r[:2])
		}
		return string(r)
	}
	return "?"
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// avatarClass picks one of six avatar tints, stable per address.
func avatarClass(s string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(s)))
	return "c" + strconv.Itoa(int(h.Sum32()%6))
}

func humanizeBytes(b int64) string {
	switch {
	case b >= 1<<30:
		v := float64(b) / (1 << 30)
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d GB", int64(v))
		}
		return fmt.Sprintf("%.1f GB", v)
	case b >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// quotaKey renders a byte count in the "500M"/"2G" form the quota presets use.
func quotaKey(b int64) string {
	switch {
	case b > 0 && b%(1<<30) == 0:
		return strconv.FormatInt(b/(1<<30), 10) + "G"
	case b > 0 && b%(1<<20) == 0:
		return strconv.FormatInt(b/(1<<20), 10) + "M"
	}
	return strconv.FormatInt(b, 10)
}

func percent(part, whole int64) int {
	if whole <= 0 {
		return 0
	}
	p := int(part * 100 / whole)
	if p > 100 {
		p = 100
	}
	if p == 0 && part > 0 {
		p = 1
	}
	return p
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d.Hours()/24), "day") + " ago"
	}
	return t.Format("Jan 2, 2006")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	// Pluralize the last word: "alias" → "aliases", "external member" → "external members".
	suffix := "s"
	if strings.HasSuffix(noun, "s") || strings.HasSuffix(noun, "x") || strings.HasSuffix(noun, "sh") {
		suffix = "es"
	}
	return strconv.Itoa(n) + " " + noun + suffix
}

var policyLabels = map[string][2]string{
	mailbox.PostAnyone:       {"Anyone", "Anyone on the internet can email the group."},
	mailbox.PostOrganization: {"Organization", "Any signed-in user on this server can post."},
	mailbox.PostMembers:      {"Members only", "Only the group's members can post."},
	mailbox.PostOwners:       {"Owners only", "Only owners can post. Good for announcement lists."},
}

func policyLabel(p string) string { return policyLabels[p][0] }
func policyDesc(p string) string  { return policyLabels[p][1] }

func policyTone(p string) string {
	switch p {
	case mailbox.PostAnyone:
		return "orange"
	case mailbox.PostOwners:
		return "purple"
	case mailbox.PostMembers:
		return "blue"
	}
	return "green"
}

// auditIcon/auditTone give an audit action a recognizable icon and color in lists.
func auditIcon(action string) string {
	switch {
	case strings.HasPrefix(action, "user") || strings.HasPrefix(action, "mailbox"):
		return "user"
	case strings.HasPrefix(action, "group"):
		return "group"
	case strings.HasPrefix(action, "domain"):
		return "globe"
	case strings.HasPrefix(action, "alias"):
		return "at"
	case strings.HasPrefix(action, "admin") || strings.HasPrefix(action, "webadmin"):
		return "shield"
	case strings.HasPrefix(action, "api") || strings.HasPrefix(action, "token"):
		return "key"
	case strings.HasPrefix(action, "dkim"):
		return "lock"
	}
	return "activity"
}

func auditTone(action string) string {
	switch {
	case strings.Contains(action, "delete") || strings.Contains(action, "remove") || strings.Contains(action, "revoke") || strings.Contains(action, "suspend"):
		return "red"
	case strings.Contains(action, "create") || strings.Contains(action, "add") || strings.Contains(action, "generate") || strings.Contains(action, "import"):
		return "green"
	}
	return "blue"
}

// dict builds a map for passing several values into a sub-template.
func dict(kv ...any) map[string]any {
	m := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		m[k] = kv[i+1]
	}
	return m
}

var funcMap = template.FuncMap{
	"mailHost":     mailHostFunc,
	"assetVersion": func() string { return web.AssetVersion },
	"icon":         iconFunc,
	"initials":     initials,
	"avatarClass":  avatarClass,
	"bytes":        humanizeBytes,
	"percent":      percent,
	"timeAgo":      timeAgo,
	"plural":       plural,
	"policyLabel":  policyLabel,
	"policyDesc":   policyDesc,
	"policyTone":   policyTone,
	"policies":     func() []string { return mailbox.PostPolicies },
	"auditIcon":    auditIcon,
	"auditTone":    auditTone,
	"dict":         dict,
	"lower":        strings.ToLower,
	"localPart": func(email string) string {
		l, _, _ := strings.Cut(email, "@")
		return l
	},
	"domainOf": func(email string) string {
		_, d, _ := strings.Cut(email, "@")
		return d
	},
	"pathEscape": url.PathEscape,
	"add":        func(a, b int) int { return a + b },
	"quotaKey":   quotaKey,
	"list":       func(v ...int) []int { return v },
	"inChoices": func(v string, choices []string) bool {
		for _, c := range choices {
			if c == v {
				return true
			}
		}
		return false
	},
}

func init() {
	iconTemplates = template.Must(template.New("icons.html").ParseFS(web.TemplatesFS, "templates/icons.html"))
	shared := []string{"templates/layout.html", "templates/icons.html", "templates/components.html"}
	for name, file := range pageFiles {
		pageTemplates[name] = template.Must(template.New("layout.html").Funcs(funcMap).ParseFS(web.TemplatesFS, append(shared, file)...))
	}
	loginTemplate = template.Must(template.New("login.html").Funcs(funcMap).ParseFS(web.TemplatesFS, "templates/login.html", "templates/icons.html"))
}

func renderPage(w http.ResponseWriter, page string, data any) {
	tmpl, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("webui: render %s: %v", page, err)
		http.Error(w, "could not render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func renderPartial(w http.ResponseWriter, page, block string, data any) {
	tmpl, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, block, data); err != nil {
		log.Printf("webui: render %s/%s: %v", page, block, err)
		http.Error(w, "could not render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func renderLogin(w http.ResponseWriter, status int, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = loginTemplate.ExecuteTemplate(w, "login", map[string]any{"Error": errMsg})
}

// --- flash messages, redirects, errors -------------------------------------------

const flashCookie = "pb_flash"

// base builds a page's Base, consuming any flash message left by the previous request.
func (s *Server) base(w http.ResponseWriter, r *http.Request, title, active string, crumbs ...Crumb) Base {
	b := Base{Title: title, Active: active, Crumbs: crumbs}
	if u := userFromContext(r); u != nil {
		b.Username = u.Username
	}
	if c, err := r.Cookie(flashCookie); err == nil {
		if raw, err := url.QueryUnescape(c.Value); err == nil {
			var f Flash
			if json.Unmarshal([]byte(raw), &f) == nil && f.Message != "" {
				b.Flash = &f
			}
		}
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	}
	return b
}

func setFlash(w http.ResponseWriter, kind, msg string) {
	raw, _ := json.Marshal(Flash{Kind: kind, Message: msg})
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: url.QueryEscape(string(raw)), Path: "/",
		MaxAge: 60, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

// done finishes a successful change: shows msg as a toast on the page it navigates to.
func done(w http.ResponseWriter, r *http.Request, to, msg string) {
	if msg != "" {
		setFlash(w, "success", msg)
	}
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// fail reports a failed change. htmx requests get the message as a toast (static/app.js
// shows non-2xx response bodies); plain form posts get a plain error page.
func fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg))
}

// failErr maps a store error: caller mistakes are shown, anything else is logged and
// reported generically.
func failErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case mailbox.IsValidation(err):
		fail(w, r, http.StatusBadRequest, err.Error())
	case isSentinel(err, mailbox.ErrNotFound):
		fail(w, r, http.StatusNotFound, err.Error())
	case isSentinel(err, mailbox.ErrExists):
		fail(w, r, http.StatusConflict, err.Error())
	default:
		log.Printf("webui: %s %s: %v", r.Method, r.URL.Path, err)
		fail(w, r, http.StatusInternalServerError, "Something went wrong. The details are in the server log (journalctl -u patrabahokd).")
	}
}

// --- pagination -----------------------------------------------------------------

type Pager struct {
	Page, Size, Total int
	base              url.Values
	path              string
}

func newPager(r *http.Request, size, total int) Pager {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	return Pager{Page: page, Size: size, Total: total, base: r.URL.Query(), path: r.URL.Path}
}

func pageFromRequest(r *http.Request) int {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		return 1
	}
	return page
}

func (p Pager) Offset() int { return (p.Page - 1) * p.Size }
func (p Pager) From() int {
	if p.Total == 0 {
		return 0
	}
	return p.Offset() + 1
}
func (p Pager) To() int {
	if t := p.Offset() + p.Size; t < p.Total {
		return t
	}
	return p.Total
}
func (p Pager) HasPrev() bool { return p.Page > 1 }
func (p Pager) HasNext() bool { return p.Offset()+p.Size < p.Total }
func (p Pager) url(page int) string {
	q := url.Values{}
	for k, v := range p.base {
		q[k] = v
	}
	q.Set("page", strconv.Itoa(page))
	return p.path + "?" + q.Encode()
}
func (p Pager) PrevURL() string { return p.url(p.Page - 1) }
func (p Pager) NextURL() string { return p.url(p.Page + 1) }
