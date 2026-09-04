package webui

import (
	"html/template"
	"net/http"

	"github.com/itsrifathridoy/patrabahok/cli/web"
)

type Base struct {
	Title    string
	Active   string
	Username string
}

var pageFiles = map[string]string{
	"domains":   "templates/domains.html",
	"mailboxes": "templates/mailboxes.html",
	"aliases":   "templates/aliases.html",
	"dkim":      "templates/dkim.html",
	"queue":     "templates/queue.html",
	"admins":    "templates/admins.html",
	"settings":  "templates/settings.html",
}

var pageTemplates = map[string]*template.Template{}
var loginTemplate *template.Template

func init() {
	for name, file := range pageFiles {
		pageTemplates[name] = template.Must(template.ParseFS(web.TemplatesFS, "templates/layout.html", file))
	}
	loginTemplate = template.Must(template.ParseFS(web.TemplatesFS, "templates/login.html"))
}

func renderPage(w http.ResponseWriter, page string, data any) {
	tmpl, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func renderPartial(w http.ResponseWriter, page, block string, data any) {
	tmpl, ok := pageTemplates[page]
	if !ok {
		http.Error(w, "template not found: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, block, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func renderLogin(w http.ResponseWriter, status int, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = loginTemplate.ExecuteTemplate(w, "login", map[string]any{"Error": errMsg})
}
