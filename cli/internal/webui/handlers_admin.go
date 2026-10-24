package webui

import (
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/internal/adminauth"
)

type AdminsPageData struct {
	Base
	Admins      []adminauth.AdminUser
	CurrentUser string
}

func (s *Server) handleAdminsPage(w http.ResponseWriter, r *http.Request) {
	list, err := s.admins.ListUsers(r.Context())
	if err != nil {
		failErr(w, r, err)
		return
	}
	renderPage(w, "admins", AdminsPageData{
		Base:        s.base(w, r, "Administrators", "admins", Crumb{Label: "Security"}, Crumb{Label: "Administrators"}),
		Admins:      list,
		CurrentUser: userFromContext(r).Username,
	})
}

func (s *Server) handleAdminAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		fail(w, r, http.StatusBadRequest, "Invalid form submission.")
		return
	}
	username := formValue(r, "username")
	password := r.FormValue("password")
	if len(password) < 8 {
		fail(w, r, http.StatusBadRequest, "The password must be at least 8 characters.")
		return
	}
	if err := s.admins.CreateUser(r.Context(), username, password); err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.record(r, "admin.create", username, "")
	done(w, r, "/admins", "Administrator "+username+" added.")
}

func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("username")
	if target == userFromContext(r).Username {
		fail(w, r, http.StatusBadRequest, "You can't remove your own account while signed in.")
		return
	}
	if err := s.admins.DeleteUser(r.Context(), target); err != nil {
		fail(w, r, http.StatusBadRequest, err.Error())
		return
	}
	s.record(r, "admin.delete", target, "")
	done(w, r, "/admins", "Administrator "+target+" removed.")
}
