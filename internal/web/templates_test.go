package web

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestAdminTemplatesRender(t *testing.T) {
	templates := NewTemplates()
	for name, data := range renderableTemplateCases() {
		recorder := httptest.NewRecorder()
		if err := templates.Render(recorder, name, data); err != nil {
			t.Errorf("render %s: %v", name, err)
		}
	}
}

func renderableTemplateCases() map[string]any {
	return map[string]any{
		"admin_login":     map[string]any{"Title": "t", "AuthorizeURL": "/login"},
		"admin_forbidden": map[string]any{"Title": "t", "Path": "/admin/users"},
		"transition_page": TransitionPageData{
			Title: "t", Heading: "h", Description: "d", Target: "/admin",
		},
		"server_home": map[string]any{"Title": "Server"},
	}
}

func TestRenderedPagesCarryNoInlineScript(t *testing.T) {
	t.Parallel()
	inlineScript := regexp.MustCompile(`(?i)<script(?:\s[^>]*)?>[^<]`)
	for name, data := range renderableTemplateCases() {
		recorder := httptest.NewRecorder()
		if err := NewTemplates().Render(recorder, name, data); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if match := inlineScript.FindString(recorder.Body.String()); match != "" {
			t.Errorf("%s carries an inline script near %q", name, match)
		}
	}
}

func TestEveryPostFormCarriesCSRFField(t *testing.T) {
	t.Parallel()
	openingForm := regexp.MustCompile(`(?i)<form[^>]*method="post"[^>]*>`)
	for name, data := range renderableTemplateCases() {
		values, ok := data.(map[string]any)
		if !ok {
			continue
		}
		values["CSRFToken"] = "token-value"
		recorder := httptest.NewRecorder()
		if err := NewTemplates().Render(recorder, name, values); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		body := recorder.Body.String()
		forms := openingForm.FindAllStringIndex(body, -1)
		for _, form := range forms {
			field := `<input type="hidden" name="csrf_token" value="token-value">`
			if !strings.HasPrefix(body[form[1]:], field) {
				t.Errorf("%s has a POST form without the CSRF field: %s", name, body[form[0]:form[1]])
			}
		}
	}
}

func TestAdminLoginRendersBandBBSButton(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	if err := NewTemplates().Render(recorder, "admin_login", map[string]any{"Title": "管理后台", "AuthorizeURL": "/oauth"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(recorder.Body.String(), `href="/oauth"`) {
		t.Fatal("login page is missing the BandBBS authorize link")
	}
}
