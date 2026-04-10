package server

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/yay101/mediarr/config"
	"github.com/yay101/mediarr/db"
)

type TemplateData struct {
	User      *db.User
	Config    *config.Config
	PageTitle string
	IsAdmin   bool
}

type TemplateServer struct {
	templates *template.Template
	webFS     embed.FS
	webPath   string
}

func NewTemplateServer(webFS embed.FS, webPath string) *TemplateServer {
	return &TemplateServer{
		webFS:   webFS,
		webPath: webPath,
	}
}

func (ts *TemplateServer) Parse() error {
	funcMap := template.FuncMap{
		"escape": func(s string) string {
			return template.HTMLEscapeString(s)
		},
	}

	tmpl, err := template.New("layout.html").Funcs(funcMap).ParseGlob(path.Join(ts.webPath, "*.html"))
	if err != nil {
		return err
	}

	ts.templates = tmpl
	slog.Info("templates parsed", "count", len(tmpl.DefinedTemplates()))
	return nil
}

func (ts *TemplateServer) RenderPage(w http.ResponseWriter, r *http.Request, page string, data *TemplateData) error {
	if ts.templates == nil {
		http.Error(w, "Templates not loaded", http.StatusInternalServerError)
		return nil
	}

	data.PageTitle = strings.Title(strings.TrimPrefix(page, "/"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return ts.templates.ExecuteTemplate(w, "layout", data)
}

func (ts *TemplateServer) Templates() *template.Template {
	return ts.templates
}
