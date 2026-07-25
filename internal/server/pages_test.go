package server

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/web"
)

func TestAuthFailedUsesSafeFriendlyMessage(t *testing.T) {
	t.Parallel()
	app := &App{templates: web.NewTemplates()}
	request := httptest.NewRequest(http.MethodGet, "/auth/failed?error=%3Cscript%3Ealert(1)%3C/script%3E", nil)
	recorder := httptest.NewRecorder()

	app.handleAuthFailed(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "alert(1)") {
		t.Fatal("raw OAuth error was reflected into the response")
	}
	if !strings.Contains(body, "未能完成授权，请返回 OronBox 后重试") {
		t.Fatal("friendly fallback error message is missing")
	}
	for name, expected := range map[string]string{
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"X-Robots-Tag":            "noindex, nofollow",
		"Content-Security-Policy": "fonts.loli.net",
	} {
		if value := recorder.Header().Get(name); !strings.Contains(value, expected) {
			t.Errorf("%s = %q, want it to contain %q", name, value, expected)
		}
	}
}

func TestOpenRendersSharedTransitionPage(t *testing.T) {
	t.Parallel()
	app := &App{templates: web.NewTemplates()}
	request := httptest.NewRequest(http.MethodGet, "/open?source=deviceQr&name=RW5E&mac=0c07dff5a0f6&authkey=secret", nil)
	recorder := httptest.NewRecorder()

	app.handleOpen(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		`在 OronBox 中打开`,
		`oronbox://open?`,
		`mac=0C07DFF5A0F6`,
		`authkey=secret`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("open page is missing %q\n%s", expected, body)
		}
	}
	if recorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", recorder.Header().Get("Referrer-Policy"))
	}
}

func TestLoginSuccessTransitionRendersBeforeDeepLink(t *testing.T) {
	t.Parallel()
	app := &App{templates: web.NewTemplates()}
	recorder := httptest.NewRecorder()

	app.renderTransition(recorder, web.TransitionPageData{
		Title:       "登录成功",
		Heading:     "登录成功",
		Description: "正在返回 OronBox",
		ButtonLabel: "打开 OronBox",
		Target:      template.URL("oronbox://oauth/callback?ticket=one-time"),
		Auto:        true,
		Tone:        "success",
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("transition unexpectedly redirected directly to %q", location)
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"登录成功",
		"正在返回 OronBox",
		"oronbox://oauth/callback?ticket=one-time",
		"location.replace",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("login transition is missing %q", expected)
		}
	}
}

func TestLoginTransitionTargetRejectsUntrustedReturnURI(t *testing.T) {
	t.Parallel()
	app := &App{}

	if _, err := app.loginTransitionTarget("javascript:alert(1)", "one-time"); err == nil {
		t.Fatal("untrusted OAuth return URI was accepted")
	}
}

func TestLoginTransitionTargetAllowsConfiguredNativeCallback(t *testing.T) {
	t.Parallel()
	app := &App{}
	app.cfg.ClientRedirectURI = "oronbox://oauth/callback"

	target, err := app.loginTransitionTarget(app.cfg.ClientRedirectURI, "one-time")
	if err != nil {
		t.Fatalf("configured native callback was rejected: %v", err)
	}
	if got := string(target); got != "oronbox://oauth/callback?ticket=one-time" {
		t.Fatalf("target = %q", got)
	}
}
