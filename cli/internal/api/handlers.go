package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
	"github.com/itsrifathridoy/patrabahok/cli/internal/sysinfo"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func statusForErr(err error) int {
	if errors.Is(err, mailbox.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	s.mux.HandleFunc("GET /v1/status", s.requireScope("status", s.handleStatus))

	s.mux.HandleFunc("GET /v1/domains", s.requireScope("domain", s.handleDomainList))
	s.mux.HandleFunc("POST /v1/domains", s.requireScope("domain", s.handleDomainAdd))
	s.mux.HandleFunc("DELETE /v1/domains/{name}", s.requireScope("domain", s.handleDomainRemove))

	s.mux.HandleFunc("GET /v1/mailboxes", s.requireScope("mailbox", s.handleMailboxList))
	s.mux.HandleFunc("POST /v1/mailboxes", s.requireScope("mailbox", s.handleMailboxAdd))
	s.mux.HandleFunc("DELETE /v1/mailboxes/{email}", s.requireScope("mailbox", s.handleMailboxRemove))
	s.mux.HandleFunc("PUT /v1/mailboxes/{email}/password", s.requireScope("mailbox", s.handleMailboxPasswd))
	s.mux.HandleFunc("PUT /v1/mailboxes/{email}/quota", s.requireScope("mailbox", s.handleMailboxQuota))

	s.mux.HandleFunc("GET /v1/aliases", s.requireScope("alias", s.handleAliasList))
	s.mux.HandleFunc("POST /v1/aliases", s.requireScope("alias", s.handleAliasAdd))
	s.mux.HandleFunc("DELETE /v1/aliases", s.requireScope("alias", s.handleAliasRemove))

	s.mux.HandleFunc("GET /v1/dkim/{domain}", s.requireScope("dkim", s.handleDKIM))
	s.mux.HandleFunc("GET /v1/dns/{domain}", s.requireScope("dns", s.handleDNS))

	s.mux.HandleFunc("GET /v1/queue", s.requireScope("queue", s.handleQueueList))
	s.mux.HandleFunc("POST /v1/queue/flush", s.requireScope("queue", s.handleQueueFlush))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	st, err := sysinfo.InstallerState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDomainList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.DomainList(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDomainAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.store.DomainAdd(r.Context(), body.Name); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": body.Name})
}

func (s *Server) handleDomainRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := s.store.DomainRemove(r.Context(), name); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMailboxList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.MailboxList(r.Context(), r.URL.Query().Get("domain"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMailboxAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		QuotaBytes int64  `json:"quota_bytes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.QuotaBytes <= 0 {
		body.QuotaBytes = 1 << 30 // 1G default
	}
	if err := s.store.MailboxAdd(r.Context(), body.Email, body.Password, body.QuotaBytes); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"email": body.Email})
}

func (s *Server) handleMailboxRemove(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if err := s.store.MailboxRemove(r.Context(), email); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMailboxPasswd(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.store.MailboxPasswd(r.Context(), email, body.Password); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMailboxQuota(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	var body struct {
		QuotaBytes int64  `json:"quota_bytes"`
		Quota      string `json:"quota"` // e.g. "2G" — an alternative to quota_bytes
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	quotaBytes := body.QuotaBytes
	if quotaBytes <= 0 && body.Quota != "" {
		parsed, err := mailbox.ParseQuota(body.Quota)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		quotaBytes = parsed
	}
	if err := s.store.MailboxSetQuota(r.Context(), email, quotaBytes); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAliasList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.AliasList(r.Context(), r.URL.Query().Get("domain"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleAliasAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.store.AliasAdd(r.Context(), body.Source, body.Destination); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, body)
}

func (s *Server) handleAliasRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source      string `json:"source"`
		Destination string `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.store.AliasRemove(r.Context(), body.Source, body.Destination); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDKIM(w http.ResponseWriter, r *http.Request) {
	rec, err := sysinfo.DKIMRecord(r.PathValue("domain"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"record": rec})
}

func (s *Server) handleDNS(w http.ResponseWriter, r *http.Request) {
	rec, err := sysinfo.DNSRecords(r.PathValue("domain"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"records": rec})
}

func (s *Server) handleQueueList(w http.ResponseWriter, r *http.Request) {
	out, err := sysinfo.QueueList()
	if err != nil && strings.TrimSpace(out) == "" {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"queue": out})
}

func (s *Server) handleQueueFlush(w http.ResponseWriter, r *http.Request) {
	out, err := sysinfo.QueueFlush()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"result": out})
}
