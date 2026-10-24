// Package publicapi implements the internet-facing, per-domain management API served by
// patrabahokd's public HTTPS listener (https://<mail hostname>/api/...). Unlike the local
// JSON API (internal/api, Unix socket, server-wide tokens), every request here is
// authenticated with a domain's own API key (internal/domainkey) and can only ever touch
// that one domain's mailboxes and aliases — no domain, DKIM, queue, or server operations.
// Intended for server-to-server integration (e.g. a school's student system creating
// mailboxes), so there is deliberately no CORS support: an API key belongs on a backend,
// never in a browser.
package publicapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/itsrifathridoy/patrabahok/cli/internal/audit"
	"github.com/itsrifathridoy/patrabahok/cli/internal/domainkey"
	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

const (
	maxBodyBytes      = 64 << 10
	minPasswordLength = 8
	maxPasswordLength = 128
	defaultQuota      = "1G"
)

// usernameRe is the local part this API accepts: lowercase letters, digits, and . _ -
// (not at the start/end). Deliberately stricter than what SMTP technically permits, so
// every address the API creates is one any mail client will handle.
var usernameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)

type Server struct {
	store *mailbox.Store
	keys  *domainkey.Store
	audit *audit.Store
	mux   *http.ServeMux
}

func New(db *sql.DB) *Server {
	s := &Server{
		store: mailbox.NewStore(db),
		keys:  domainkey.NewStore(db),
		audit: audit.NewStore(db),
		mux:   http.NewServeMux(),
	}
	s.routes()
	s.groupRoutes()
	return s
}

// record writes an audit entry attributed to the request's API key.
func (s *Server) record(r *http.Request, action, target, detail string) {
	s.audit.Record(r.Context(), "apikey:"+domainOf(r), action, target, detail, clientIP(r))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	w.Header().Set("Cache-Control", "no-store")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/{domain}/mailbox", s.auth(s.handleMailboxList))
	s.mux.HandleFunc("POST /api/{domain}/mailbox", s.auth(s.handleMailboxCreate))
	s.mux.HandleFunc("GET /api/{domain}/mailbox/{username}", s.auth(s.handleMailboxGet))
	s.mux.HandleFunc("DELETE /api/{domain}/mailbox/{username}", s.auth(s.handleMailboxDelete))
	s.mux.HandleFunc("PUT /api/{domain}/mailbox/{username}/password", s.auth(s.handleMailboxPassword))
	s.mux.HandleFunc("PUT /api/{domain}/mailbox/{username}/name", s.auth(s.handleMailboxName))
	s.mux.HandleFunc("PUT /api/{domain}/mailbox/{username}/quota", s.auth(s.handleMailboxQuota))
	s.mux.HandleFunc("PUT /api/{domain}/mailbox/{username}/status", s.auth(s.handleMailboxStatus))

	s.mux.HandleFunc("GET /api/{domain}/alias", s.auth(s.handleAliasList))
	s.mux.HandleFunc("POST /api/{domain}/alias", s.auth(s.handleAliasCreate))
	s.mux.HandleFunc("DELETE /api/{domain}/alias", s.auth(s.handleAliasDelete))

	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
}

// --- auth -------------------------------------------------------------------------

type ctxKey int

const domainCtxKey ctxKey = iota

func presentedKey(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get("X-API-Key")); k != "" {
		return k
	}
	const prefix = "Bearer "
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(auth, prefix))
	}
	return ""
}

// auth requires a valid API key belonging to exactly the domain named in the URL path.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		domain := strings.ToLower(r.PathValue("domain"))
		key := presentedKey(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, "missing API key (send it as 'Authorization: Bearer <key>' or 'X-API-Key: <key>')")
			return
		}
		keyDomain, err := s.keys.Verify(r.Context(), key)
		if err != nil {
			if !errors.Is(err, domainkey.ErrInvalidKey) {
				log.Printf("publicapi: key lookup: %v", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			log.Printf("publicapi: rejected invalid API key from %s for %s", clientIP(r), domain)
			writeError(w, http.StatusUnauthorized, "invalid or revoked API key")
			return
		}
		if !strings.EqualFold(keyDomain, domain) {
			log.Printf("publicapi: rejected %s's API key for %s from %s", keyDomain, domain, clientIP(r))
			writeError(w, http.StatusForbidden, "this API key is not valid for domain "+domain)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), domainCtxKey, keyDomain)))
	}
}

func domainOf(r *http.Request) string {
	d, _ := r.Context().Value(domainCtxKey).(string)
	return d
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i != -1 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}

// --- helpers ------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeStoreError maps a mailbox.Store error to a response. Validation errors are the
// caller's fault and are reported as-is; anything else is logged and kept vague.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, mailbox.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, mailbox.ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	case mailbox.IsValidation(err):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("publicapi: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

// address validates the {username} path segment (or a body field) and returns the full
// address within the request's domain.
func address(w http.ResponseWriter, r *http.Request, username string) (string, bool) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernameRe.MatchString(username) || strings.Contains(username, "..") {
		writeError(w, http.StatusBadRequest, "invalid username: use 1-64 lowercase letters, digits, '.', '_' or '-', starting and ending with a letter or digit")
		return "", false
	}
	return username + "@" + domainOf(r), true
}

func validPassword(w http.ResponseWriter, password string) bool {
	n := utf8.RuneCountInString(password)
	if n < minPasswordLength || n > maxPasswordLength {
		writeError(w, http.StatusBadRequest, "password must be between 8 and 128 characters")
		return false
	}
	return true
}

func validName(w http.ResponseWriter, name string) bool {
	if utf8.RuneCountInString(name) > 255 || strings.ContainsAny(name, "\r\n") {
		writeError(w, http.StatusBadRequest, "name must be a single line of at most 255 characters")
		return false
	}
	return true
}

type mailboxView struct {
	Email      string `json:"email"`
	Username   string `json:"username"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	QuotaBytes int64  `json:"quota_bytes"`
}

func viewOf(m mailbox.Mailbox) mailboxView {
	local, _, _ := strings.Cut(m.Email, "@")
	return mailboxView{Email: m.Email, Username: local, Name: m.DisplayName, Enabled: m.Enabled, QuotaBytes: m.QuotaBytes}
}

// --- mailboxes ----------------------------------------------------------------------

func (s *Server) handleMailboxList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.MailboxList(r.Context(), domainOf(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	out := make([]mailboxView, 0, len(list))
	for _, m := range list {
		out = append(out, viewOf(m))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMailboxCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Name     string `json:"name"`
		Quota    string `json:"quota"` // e.g. "500M", "2G"; default 1G
	}
	if !decode(w, r, &body) {
		return
	}
	email, ok := address(w, r, body.Username)
	if !ok || !validPassword(w, body.Password) || !validName(w, body.Name) {
		return
	}
	if body.Quota == "" {
		body.Quota = defaultQuota
	}
	quota, err := mailbox.ParseQuota(body.Quota)
	if err != nil || quota <= 0 {
		writeError(w, http.StatusBadRequest, "invalid quota (expected e.g. 500M, 2G)")
		return
	}
	if err := s.store.MailboxCreate(r.Context(), email, body.Password, strings.TrimSpace(body.Name), quota); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondMailbox(w, r, http.StatusCreated, email)
}

func (s *Server) respondMailbox(w http.ResponseWriter, r *http.Request, status int, email string) {
	m, err := s.store.MailboxGet(r.Context(), email)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, status, viewOf(*m))
}

func (s *Server) handleMailboxGet(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	s.respondMailbox(w, r, http.StatusOK, email)
}

func (s *Server) handleMailboxDelete(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	if _, err := s.store.MailboxGet(r.Context(), email); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.MailboxRemove(r.Context(), email); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMailboxPassword(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &body) || !validPassword(w, body.Password) {
		return
	}
	if err := s.store.MailboxPasswd(r.Context(), email, body.Password); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondMailbox(w, r, http.StatusOK, email)
}

func (s *Server) handleMailboxName(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &body) || !validName(w, body.Name) {
		return
	}
	if err := s.store.MailboxSetName(r.Context(), email, strings.TrimSpace(body.Name)); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondMailbox(w, r, http.StatusOK, email)
}

func (s *Server) handleMailboxQuota(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	var body struct {
		Quota string `json:"quota"`
	}
	if !decode(w, r, &body) {
		return
	}
	quota, err := mailbox.ParseQuota(body.Quota)
	if err != nil || body.Quota == "" || quota <= 0 {
		writeError(w, http.StatusBadRequest, "invalid quota (expected e.g. 500M, 2G)")
		return
	}
	if err := s.store.MailboxSetQuota(r.Context(), email, quota); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondMailbox(w, r, http.StatusOK, email)
}

func (s *Server) handleMailboxStatus(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Enabled == nil {
		writeError(w, http.StatusBadRequest, `"enabled" (true or false) is required`)
		return
	}
	if err := s.store.MailboxSetEnabled(r.Context(), email, *body.Enabled); err != nil {
		writeStoreError(w, err)
		return
	}
	s.respondMailbox(w, r, http.StatusOK, email)
}

// --- aliases ------------------------------------------------------------------------

type aliasBody struct {
	Source      string `json:"source"`      // local part within this domain, e.g. "info"
	Destination string `json:"destination"` // any full address
}

func (s *Server) handleAliasList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.AliasList(r.Context(), domainOf(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if list == nil {
		list = []mailbox.Alias{}
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) aliasFromBody(w http.ResponseWriter, r *http.Request) (source, destination string, ok bool) {
	var body aliasBody
	if !decode(w, r, &body) {
		return "", "", false
	}
	source, ok = address(w, r, body.Source)
	if !ok {
		return "", "", false
	}
	destination = strings.ToLower(strings.TrimSpace(body.Destination))
	if err := mailbox.ValidateEmail(destination); err != nil {
		writeError(w, http.StatusBadRequest, "invalid destination address")
		return "", "", false
	}
	return source, destination, true
}

func (s *Server) handleAliasCreate(w http.ResponseWriter, r *http.Request) {
	source, destination, ok := s.aliasFromBody(w, r)
	if !ok {
		return
	}
	if err := s.store.AliasAdd(r.Context(), source, destination); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mailbox.Alias{Source: source, Destination: destination})
}

func (s *Server) handleAliasDelete(w http.ResponseWriter, r *http.Request) {
	source, destination, ok := s.aliasFromBody(w, r)
	if !ok {
		return
	}
	if err := s.store.AliasRemove(r.Context(), source, destination); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
