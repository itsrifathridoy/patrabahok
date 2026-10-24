package api

import (
	"encoding/json"
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

func (s *Server) groupRoutes() {
	s.mux.HandleFunc("GET /v1/groups", s.requireScope("group", s.handleGroupList))
	s.mux.HandleFunc("POST /v1/groups", s.requireScope("group", s.handleGroupCreate))
	s.mux.HandleFunc("GET /v1/groups/{email}", s.requireScope("group", s.handleGroupGet))
	s.mux.HandleFunc("DELETE /v1/groups/{email}", s.requireScope("group", s.handleGroupDelete))
	s.mux.HandleFunc("POST /v1/groups/{email}/members", s.requireScope("group", s.handleGroupAddMembers))
	s.mux.HandleFunc("DELETE /v1/groups/{email}/members/{member}", s.requireScope("group", s.handleGroupRemoveMember))
}

func (s *Server) handleGroupList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.GroupList(r.Context(), r.URL.Query().Get("domain"), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email         string `json:"email"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		PostPolicy    string `json:"post_policy"`
		AllowExternal bool   `json:"allow_external"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.PostPolicy == "" {
		body.PostPolicy = mailbox.PostOrganization
	}
	if body.Name == "" {
		body.Name = body.Email
	}
	settings := mailbox.GroupSettings{Name: body.Name, Description: body.Description, PostPolicy: body.PostPolicy, AllowExternal: body.AllowExternal}
	if err := s.store.GroupCreate(r.Context(), body.Email, settings); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	s.handleGroupGetEmail(w, r, http.StatusCreated, body.Email)
}

func (s *Server) handleGroupGet(w http.ResponseWriter, r *http.Request) {
	s.handleGroupGetEmail(w, r, http.StatusOK, r.PathValue("email"))
}

func (s *Server) handleGroupGetEmail(w http.ResponseWriter, r *http.Request, status int, email string) {
	g, err := s.store.GroupGet(r.Context(), email)
	if err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	members, err := s.store.GroupMembers(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, status, map[string]any{"group": g, "members": members})
}

func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.store.GroupDelete(r.Context(), r.PathValue("email")); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGroupAddMembers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Members []string `json:"members"`
		Role    string   `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Role == "" {
		body.Role = mailbox.RoleMember
	}
	results, err := s.store.GroupAddMembers(r.Context(), r.PathValue("email"), body.Members, body.Role)
	if err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) handleGroupRemoveMember(w http.ResponseWriter, r *http.Request) {
	if err := s.store.GroupRemoveMember(r.Context(), r.PathValue("email"), r.PathValue("member")); err != nil {
		writeError(w, statusForErr(err), err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
