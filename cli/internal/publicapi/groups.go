package publicapi

import (
	"net/http"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

type groupView struct {
	mailbox.Group
	Members []mailbox.GroupMember `json:"members,omitempty"`
}

func (s *Server) groupRoutes() {
	s.mux.HandleFunc("GET /api/{domain}/group", s.auth(s.handleGroupList))
	s.mux.HandleFunc("POST /api/{domain}/group", s.auth(s.handleGroupCreate))
	s.mux.HandleFunc("GET /api/{domain}/group/{name}", s.auth(s.handleGroupGet))
	s.mux.HandleFunc("PUT /api/{domain}/group/{name}", s.auth(s.handleGroupUpdate))
	s.mux.HandleFunc("DELETE /api/{domain}/group/{name}", s.auth(s.handleGroupDelete))
	s.mux.HandleFunc("POST /api/{domain}/group/{name}/members", s.auth(s.handleGroupAddMembers))
	s.mux.HandleFunc("DELETE /api/{domain}/group/{name}/members/{address}", s.auth(s.handleGroupRemoveMember))

	s.mux.HandleFunc("PUT /api/{domain}/mailbox/{username}/forward", s.auth(s.handleForwardSet))
	s.mux.HandleFunc("DELETE /api/{domain}/mailbox/{username}/forward", s.auth(s.handleForwardClear))
}

type groupBody struct {
	Name          string `json:"name"` // the local part, e.g. "class10"
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	PostPolicy    string `json:"post_policy"`
	AllowExternal bool   `json:"allow_external"`
}

func (b groupBody) settings() mailbox.GroupSettings {
	return mailbox.GroupSettings{Name: b.DisplayName, Description: b.Description, PostPolicy: b.PostPolicy, AllowExternal: b.AllowExternal}
}

func (s *Server) respondGroup(w http.ResponseWriter, r *http.Request, status int, email string) {
	g, err := s.store.GroupGet(r.Context(), email)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	members, err := s.store.GroupMembers(r.Context(), email)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, status, groupView{Group: *g, Members: members})
}

func (s *Server) handleGroupList(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.GroupList(r.Context(), domainOf(r), "")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	var body groupBody
	if !decode(w, r, &body) {
		return
	}
	email, ok := address(w, r, body.Name)
	if !ok {
		return
	}
	settings := body.settings()
	if settings.Name == "" {
		settings.Name = strings.ToLower(strings.TrimSpace(body.Name))
	}
	if settings.PostPolicy == "" {
		settings.PostPolicy = mailbox.PostOrganization
	}
	if err := s.store.GroupCreate(r.Context(), email, settings); err != nil {
		writeStoreError(w, err)
		return
	}
	s.record(r, "group.create", email, "")
	s.respondGroup(w, r, http.StatusCreated, email)
}

func (s *Server) handleGroupGet(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("name"))
	if !ok {
		return
	}
	s.respondGroup(w, r, http.StatusOK, email)
}

// handleGroupUpdate replaces the group's settings. Omitted fields keep their current
// value, except allow_external, which is a plain boolean and must always be sent.
func (s *Server) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("name"))
	if !ok {
		return
	}
	g, err := s.store.GroupGet(r.Context(), email)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	body := groupBody{DisplayName: g.Name, Description: g.Description, PostPolicy: g.PostPolicy, AllowExternal: g.AllowExternal}
	if !decode(w, r, &body) {
		return
	}
	if err := s.store.GroupUpdate(r.Context(), email, body.settings()); err != nil {
		writeStoreError(w, err)
		return
	}
	s.record(r, "group.update", email, "post_policy="+body.PostPolicy)
	s.respondGroup(w, r, http.StatusOK, email)
}

func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("name"))
	if !ok {
		return
	}
	if err := s.store.GroupDelete(r.Context(), email); err != nil {
		writeStoreError(w, err)
		return
	}
	s.record(r, "group.delete", email, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGroupAddMembers(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("name"))
	if !ok {
		return
	}
	var body struct {
		Members []string `json:"members"`
		Role    string   `json:"role"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.Members) == 0 {
		writeError(w, http.StatusBadRequest, `"members" must list at least one address`)
		return
	}
	if len(body.Members) > 1000 {
		writeError(w, http.StatusBadRequest, "at most 1000 members per request")
		return
	}
	if body.Role == "" {
		body.Role = mailbox.RoleMember
	}
	results, err := s.store.GroupAddMembers(r.Context(), email, body.Members, body.Role)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	added := 0
	for _, res := range results {
		if res.Added {
			added++
		}
	}
	s.record(r, "group.members.add", email, plural(added, "member"))
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "added": added, "failed": len(results) - added})
}

func (s *Server) handleGroupRemoveMember(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("name"))
	if !ok {
		return
	}
	member := strings.ToLower(r.PathValue("address"))
	if err := s.store.GroupRemoveMember(r.Context(), email, member); err != nil {
		writeStoreError(w, err)
		return
	}
	s.record(r, "group.members.remove", email, member)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleForwardSet(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	var body struct {
		Destination string `json:"destination"`
		KeepCopy    *bool  `json:"keep_copy"`
	}
	if !decode(w, r, &body) {
		return
	}
	keep := true
	if body.KeepCopy != nil {
		keep = *body.KeepCopy
	}
	if err := s.store.ForwardSet(r.Context(), email, body.Destination, keep); err != nil {
		writeStoreError(w, err)
		return
	}
	s.record(r, "user.forward.set", email, body.Destination)
	writeJSON(w, http.StatusOK, mailbox.Forward{Destination: strings.ToLower(strings.TrimSpace(body.Destination)), KeepCopy: keep})
}

func (s *Server) handleForwardClear(w http.ResponseWriter, r *http.Request) {
	email, ok := address(w, r, r.PathValue("username"))
	if !ok {
		return
	}
	if _, err := s.store.MailboxGet(r.Context(), email); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.store.ForwardClear(r.Context(), email); err != nil {
		writeStoreError(w, err)
		return
	}
	s.record(r, "user.forward.clear", email, "")
	w.WriteHeader(http.StatusNoContent)
}
