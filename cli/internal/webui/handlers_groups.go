package webui

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/mailbox"
)

type GroupsPageData struct {
	Base
	Groups  []mailbox.Group
	Domains []mailbox.Domain
	Query   string
	Domain  string
}

func groupURL(email string) string { return "/groups/" + url.PathEscape(email) }

func (s *Server) handleGroupsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := GroupsPageData{
		Base:   s.base(w, r, "Groups", "groups", Crumb{Label: "Groups"}),
		Query:  strings.TrimSpace(r.URL.Query().Get("q")),
		Domain: r.URL.Query().Get("domain"),
	}
	var err error
	if data.Groups, err = s.store.GroupList(ctx, data.Domain, data.Query); err != nil {
		failErr(w, r, err)
		return
	}
	if data.Domains, err = s.store.DomainList(ctx); err != nil {
		failErr(w, r, err)
		return
	}
	renderPage(w, "groups", data)
}

func groupSettingsFromForm(r *http.Request) mailbox.GroupSettings {
	return mailbox.GroupSettings{
		Name:          formValue(r, "name"),
		Description:   formValue(r, "description"),
		PostPolicy:    formValue(r, "post_policy"),
		AllowExternal: r.FormValue("allow_external") == "1",
	}
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, r, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	email := strings.ToLower(formValue(r, "local") + "@" + formValue(r, "domain"))
	settings := groupSettingsFromForm(r)
	if err := s.store.GroupCreate(r.Context(), email, settings); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "group.create", email, "posting="+settings.PostPolicy)
	done(w, r, groupURL(email), "Group "+email+" created. Add members to start delivering mail.")
}

type GroupPageData struct {
	Base
	Group     mailbox.Group
	Members   []mailbox.GroupMember
	Owners    int
	External  int
	Suspended int
	Activity  []AuditView
}

func (s *Server) handleGroupPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	email := strings.ToLower(r.PathValue("email"))
	g, err := s.store.GroupGet(ctx, email)
	if err != nil {
		if isSentinel(err, mailbox.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		failErr(w, r, err)
		return
	}
	members, err := s.store.GroupMembers(ctx, email)
	if err != nil {
		failErr(w, r, err)
		return
	}
	data := GroupPageData{
		Base:    s.base(w, r, g.Name, "groups", Crumb{Label: "Groups", Href: "/groups"}, Crumb{Label: g.Email}),
		Group:   *g,
		Members: members,
	}
	for _, m := range members {
		if m.Role == mailbox.RoleOwner {
			data.Owners++
		}
		if m.Kind == "external" {
			data.External++
		}
		if m.Suspended {
			data.Suspended++
		}
	}
	data.Activity = s.recentActivity(r, email, 8)
	renderPage(w, "group", data)
}

func (s *Server) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if err := r.ParseForm(); err != nil {
		fail(w, r, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	settings := groupSettingsFromForm(r)
	if err := s.store.GroupUpdate(r.Context(), email, settings); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "group.update", email, "posting="+settings.PostPolicy)
	done(w, r, groupURL(email), "Group settings saved.")
}

func (s *Server) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if err := s.store.GroupDelete(r.Context(), email); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "group.delete", email, "")
	done(w, r, "/groups", "Group "+email+" deleted.")
}

// splitAddresses accepts addresses separated by newlines, commas, semicolons or spaces,
// so a pasted spreadsheet column or a "To:" line both work.
func splitAddresses(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ',' || r == ';' || r == ' ' || r == '\t'
	})
}

func (s *Server) handleGroupMembersAdd(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	addresses := splitAddresses(r.FormValue("addresses"))
	if len(addresses) == 0 {
		fail(w, r, http.StatusBadRequest, "Enter at least one address.")
		return
	}
	if len(addresses) > 2000 {
		fail(w, r, http.StatusBadRequest, "At most 2000 addresses at a time.")
		return
	}
	role := formValue(r, "role")
	results, err := s.store.GroupAddMembers(r.Context(), email, addresses, role)
	if err != nil {
		failErr(w, r, err)
		return
	}
	var added int
	var problems []string
	for _, res := range results {
		if res.Added {
			added++
		} else {
			problems = append(problems, res.Address+": "+res.Error)
		}
	}
	if added > 0 {
		s.record(r, "group.members.add", email, plural(added, "member")+" ("+role+")")
	}
	if len(problems) > 0 {
		msg := fmt.Sprintf("Added %s. Couldn't add %d:\n%s", plural(added, "member"), len(problems), strings.Join(firstN(problems, 5), "\n"))
		if len(problems) > 5 {
			msg += fmt.Sprintf("\n…and %d more", len(problems)-5)
		}
		if added == 0 {
			fail(w, r, http.StatusBadRequest, msg)
			return
		}
		setFlash(w, "error", msg)
		done(w, r, groupURL(email), "")
		return
	}
	done(w, r, groupURL(email), "Added "+plural(added, "member")+".")
}

func firstN(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func (s *Server) handleGroupMemberRole(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	member := r.PathValue("member")
	role := formValue(r, "role")
	if err := s.store.GroupSetMemberRole(r.Context(), email, member, role); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "group.members.role", email, member+" → "+role)
	done(w, r, groupURL(email), member+" is now a group "+role+".")
}

func (s *Server) handleGroupMemberRemove(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	member := r.PathValue("member")
	if err := s.store.GroupRemoveMember(r.Context(), email, member); err != nil {
		failErr(w, r, err)
		return
	}
	s.record(r, "group.members.remove", email, member)
	done(w, r, groupURL(email), "Removed "+member+".")
}
