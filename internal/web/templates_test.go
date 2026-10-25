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
		"admin_forbidden": map[string]any{"Title": "t", "Path": "/console/users"},
		"transition_page": TransitionPageData{
			Title: "t", Heading: "h", Description: "d", Target: "/console",
		},
		"console_syncing": map[string]any{"Title": "正在同步前端"},
		"console_error":   map[string]any{"Title": "前端同步失败", "Error": "同步失败", "RetryAction": "/console/api/console/sync", "AssetID": 1, "ButtonLabel": "重新尝试同步"},
		"server_home":     map[string]any{"Title": "Server"},
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
