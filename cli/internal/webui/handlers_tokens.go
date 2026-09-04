package webui

import (
	"net/http"
	"strings"

	"github.com/itsrifathridoy/patrabahok/cli/internal/authtoken"
)

type TokensPageData struct {
	Base
	Tokens      []authtoken.TokenInfo
	NewToken    string
	NewTokenErr string
}

func (s *Server) tokensData(r *http.Request) (TokensPageData, error) {
	list, err := s.tokens.List(r.Context())
	if err != nil {
		return TokensPageData{}, err
	}
	return TokensPageData{
		Base:   Base{Title: "API Tokens", Active: "tokens", Username: userFromContext(r).Username},
		Tokens: list,
	}, nil
}

func (s *Server) handleTokensPage(w http.ResponseWriter, r *http.Request) {
	data, err := s.tokensData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPage(w, "tokens", data)
}

func (s *Server) handleTokenAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	var scopes []string
	if raw := strings.TrimSpace(r.FormValue("scopes")); raw != "" {
		for _, sc := range strings.Split(raw, ",") {
			if sc = strings.TrimSpace(sc); sc != "" {
				scopes = append(scopes, sc)
			}
		}
	}

	data, err := s.tokensData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	plaintext, err := s.tokens.Create(r.Context(), name, scopes)
	if err != nil {
		data.NewTokenErr = err.Error()
		renderPartial(w, "tokens", "tokens_body", data)
		return
	}
	data.NewToken = plaintext
	data.Tokens, _ = s.tokens.List(r.Context())
	renderPartial(w, "tokens", "tokens_body", data)
}

func (s *Server) handleTokenDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.tokens.Revoke(r.Context(), r.PathValue("name")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	data, err := s.tokensData(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderPartial(w, "tokens", "tokens_body", data)
}
