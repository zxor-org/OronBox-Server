package web

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

type Templates struct {
	t *template.Template
}

type TransitionPageData struct {
	Title       string
	Heading     string
	Description string
	ButtonLabel string
	Target      template.URL
	Auto        bool
	Tone        string
	CSRFToken   string
}

type AdminLoginPageData struct {
	Title        string
	AuthorizeURL string
}

type ConsoleSyncingPageData struct {
	Title string
}

type ConsoleErrorPageData struct {
	Title       string
	Error       string
	ButtonLabel string
	Target      string
}

//go:embed templates/auth.gohtml
var templateFS embed.FS

//go:embed assets/icons.svg
var iconSprite string

func iconSpriteTemplate() string {
	return `{{define "icon_sprite"}}` + iconSprite + `{{end}}`
}

func NewTemplates() *Templates {
	root := template.Must(template.New("root").Funcs(template.FuncMap{
		"eqs": func(a, b any) bool { return fmt.Sprint(a) == fmt.Sprint(b) },
		"safeURL": func(value any) string {
			trimmed := strings.TrimSpace(fmt.Sprint(value))
			lowered := strings.ToLower(trimmed)
			if strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://") || strings.HasPrefix(lowered, "oronbox://") {
				return trimmed
			}
			return ""
		},
	}).Parse(iconSpriteTemplate()))

	source, err := templateFS.ReadFile("templates/auth.gohtml")
	if err != nil {
		panic(fmt.Errorf("read auth template: %w", err))
	}
	if _, err := root.Parse(string(source)); err != nil {
		panic(fmt.Errorf("parse auth template: %w", err))
	}

	return &Templates{t: root}
}

func (t *Templates) Render(w http.ResponseWriter, name string, data any) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.t.ExecuteTemplate(w, name, data)
}
