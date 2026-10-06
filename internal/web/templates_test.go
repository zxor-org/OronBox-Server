package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransitionPageRenders(t *testing.T) {
	templates := NewTemplates()
	recorder := httptest.NewRecorder()

	data := TransitionPageData{
		Title:       "授权跳转",
		Heading:     "正在完成登录",
		Description: "正在跳转回客户端...",
		Target:      "oronbox://oauth/bandbbs?token=test",
		ButtonLabel: "打开客户端",
		Auto:        true,
		Tone:        "info",
	}

	if err := templates.Render(recorder, "transition_page", data); err != nil {
		t.Fatalf("render transition_page: %v", err)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "正在完成登录") {
		t.Errorf("rendered output missing heading: %s", body)
	}
	if !strings.Contains(body, "oronbox://oauth/bandbbs?token=test") {
		t.Errorf("rendered output missing target url: %s", body)
	}
}

func TestAdminLoginPageRenders(t *testing.T) {
	templates := NewTemplates()
	recorder := httptest.NewRecorder()

	data := AdminLoginPageData{
		Title:        "OronBox 管理后台",
		AuthorizeURL: "/admin/api/auth/bandbbs/authorize",
	}

	if err := templates.Render(recorder, "admin_login", data); err != nil {
		t.Fatalf("render admin_login: %v", err)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "OronBox 管理后台") {
		t.Errorf("rendered output missing title: %s", body)
	}
	if !strings.Contains(body, "/admin/api/auth/bandbbs/authorize") {
		t.Errorf("rendered output missing authorize url: %s", body)
	}
	if !strings.Contains(body, "i-admin_panel_settings") {
		t.Errorf("rendered output missing admin icon: %s", body)
	}
}

func TestConsoleSyncingAndErrorTemplatesRender(t *testing.T) {
	templates := NewTemplates()

	// 1. console_syncing
	rec1 := httptest.NewRecorder()
	if err := templates.Render(rec1, "console_syncing", ConsoleSyncingPageData{Title: "正在同步前端"}); err != nil {
		t.Fatalf("render console_syncing: %v", err)
	}
	body1 := rec1.Body.String()
	if !strings.Contains(body1, "正在同步前端") {
		t.Errorf("missing title in console_syncing: %s", body1)
	}
	if !strings.Contains(body1, "服务端正在从 Gitea 同步最新前端构建的页面，完成后将自动进入后台...") {
		t.Errorf("missing description in console_syncing: %s", body1)
	}
	if !strings.Contains(body1, `http-equiv="refresh"`) {
		t.Errorf("missing auto refresh meta in console_syncing: %s", body1)
	}
	if !strings.Contains(body1, "i-sync") {
		t.Errorf("missing sync icon in console_syncing: %s", body1)
	}

	// 2. console_error
	rec2 := httptest.NewRecorder()
	if err := templates.Render(rec2, "console_error", ConsoleErrorPageData{
		Title:       "前端同步失败",
		Error:       "network timeout",
		ButtonLabel: "返回管理后台",
		Target:      "/console/?dismiss=1",
	}); err != nil {
		t.Fatalf("render console_error: %v", err)
	}
	body2 := rec2.Body.String()
	if !strings.Contains(body2, "前端同步失败") || !strings.Contains(body2, "network timeout") {
		t.Errorf("missing error text in console_error: %s", body2)
	}
	if !strings.Contains(body2, `href="/console/?dismiss=1"`) || !strings.Contains(body2, "返回管理后台") {
		t.Errorf("missing return button in console_error: %s", body2)
	}
}


