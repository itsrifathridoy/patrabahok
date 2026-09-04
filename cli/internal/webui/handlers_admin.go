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

func (s *Server) adminsData(r *http.Request) (AdminsPageData, error) {
	list, err := s.admins.ListUsers(r.Context())
	if err != nil {
		return AdminsPageData{}, err
	}
	username := userFromContext(r).Username
	return AdminsPageData{
		Base:        Base{Title: "Admins", Active: "admins", Username: username},
		Admins:      list,
		CurrentUser: username,
	}, nil
}

func (s *Server) handleAdminsPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.adminsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "admins", data)
}

func (s *Server) handleAdminAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")
	if len(password) < 8 {
		http.Error(w, "password must be at least 8 characters", http.StatusBadRequest)
		return
	}
	if err := s.admins.CreateUser(r.Context(), username, password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondAdminsTable(w, r)
}

func (s *Server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	target := r.PathValue("username")
	if target == userFromContext(r).Username {
		http.Error(w, "cannot remove your own account while signed in", http.StatusBadRequest)
		return
	}
	if err := s.admins.DeleteUser(r.Context(), target); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.respondAdminsTable(w, r)
}

func (s *Server) respondAdminsTable(w http.ResponseWriter, r *http.Request) {
	data, err := s.adminsData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPartial(w, "admins", "admins_table", data)
}
