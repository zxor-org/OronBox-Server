package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func securityHeaderResponse(t *testing.T, publicURL string) http.Header {
	t.Helper()
	handler := SecurityHeaders(config.Config{PublicURL: publicURL}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin", nil))
	return recorder.Header()
}

// The console has no inline script left, so the policy must be strict enough
// that stored content reaching a template cannot execute.
func TestSecurityHeadersForbidInlineScript(t *testing.T) {
	t.Parallel()
	policy := securityHeaderResponse(t, "https://admin.example").Get("Content-Security-Policy")
	for _, expected := range []string{
		"default-src 'none'",
		"script-src 'self'",
		"form-action 'self'",
		"base-uri 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(policy, expected) {
			t.Errorf("Content-Security-Policy is missing %q: %s", expected, policy)
		}
	}
	for _, forbidden := range []string{"'unsafe-inline'", "'unsafe-eval'", "fonts.loli.net", "gstatic.loli.net"} {
		if strings.Contains(policy, forbidden) {
			t.Errorf("Content-Security-Policy still allows %q: %s", forbidden, policy)
		}
	}
}

// Pinning HSTS on a plain-HTTP deployment would lock every administrator out
// of a server that has no TLS to fall back on.
func TestStrictTransportSecurityFollowsPublicScheme(t *testing.T) {
	t.Parallel()
	if value := securityHeaderResponse(t, "https://admin.example").Get("Strict-Transport-Security"); !strings.Contains(value, "max-age=31536000") {
		t.Errorf("HTTPS deployment did not send HSTS: %q", value)
	}
	if value := securityHeaderResponse(t, "http://localhost:8080").Get("Strict-Transport-Security"); value != "" {
		t.Errorf("plain-HTTP deployment sent HSTS: %q", value)
	}
}

// A handler that already chose a stricter policy must keep it.
func TestSecurityHeadersDoNotOverrideAHandlerPolicy(t *testing.T) {
	t.Parallel()
	handler := SecurityHeaders(config.Config{PublicURL: "https://admin.example"}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", transitionCSP)
		w.WriteHeader(http.StatusOK)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/auth/success", nil))
	if got := recorder.Header().Get("Content-Security-Policy"); got != transitionCSP {
		t.Errorf("handler policy was overwritten: %q", got)
	}
}

// The error code arrives in a query parameter, so anyone can link an
// administrator to a login page carrying text of their choosing.
func TestAdminLoginErrorIsWhitelisted(t *testing.T) {
	t.Parallel()
	app := &App{cfg: config.Config{PublicURL: "https://admin.example"}, templates: web.NewTemplates()}
	recorder := httptest.NewRecorder()
	app.handleAdminLoginPage(recorder, httptest.NewRequest(http.MethodGet, "/admin/login?error=%E8%AF%B7%E8%81%94%E7%B3%BB+400-000", nil))

	body := recorder.Body.String()
	if strings.Contains(body, "400-000") {
		t.Error("login page reflected an attacker-supplied error message")
	}
	if !strings.Contains(body, "登录未能完成，请重新尝试") {
		t.Error("login page is missing the fallback error message")
	}

	known := httptest.NewRecorder()
	app.handleAdminLoginPage(known, httptest.NewRequest(http.MethodGet, "/admin/login?error=not_authorized", nil))
	if !strings.Contains(known.Body.String(), "该账号没有管理后台权限") {
		t.Error("known error code lost its specific message")
	}

	clean := httptest.NewRecorder()
	app.handleAdminLoginPage(clean, httptest.NewRequest(http.MethodGet, "/admin/login", nil))
	if strings.Contains(clean.Body.String(), "notice danger") {
		t.Error("login page shows an error without one having occurred")
	}
}

func credentialThrottleApp(failureBurst int) *App {
	cfg := config.Config{
		PublicURL: "https://admin.example",
		Limits: config.LimitsConfig{
			AuthRatePerMin: 3, AuthFailureBurst: failureBurst, AuthFailureWindow: time.Minute,
		},
	}
	return &App{
		cfg:          cfg,
		authAttempts: newWindowLimiter(cfg.Limits.AuthRatePerMin, time.Minute, 120),
		authFailures: newWindowLimiter(cfg.Limits.AuthFailureBurst, cfg.Limits.AuthFailureWindow, 20),
	}
}

func postCredentials(t *testing.T, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/oauth/bandbbs/refresh", strings.NewReader(`{}`))
	request.RemoteAddr = "203.0.113.7:41234"
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

// The attempt ceiling is the coarse backstop: it has to stop a flood even from
// a caller that never produces a failure.
func TestCredentialAttemptCeilingStopsFlooding(t *testing.T) {
	t.Parallel()
	app := credentialThrottleApp(100)
	handler := app.throttleCredentials(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	})
	for attempt := 1; attempt <= 3; attempt++ {
		if recorder := postCredentials(t, handler); recorder.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want 200", attempt, recorder.Code)
		}
	}
	recorder := postCredentials(t, handler)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}
	if recorder.Header().Get("Retry-After") == "" {
		t.Error("throttled response does not tell the client when to retry")
	}
}

// The failure budget is the one that actually ends online guessing, and it must
// be charged only by rejected credentials.
func TestCredentialFailureBudgetIgnoresSuccessfulRequests(t *testing.T) {
	t.Parallel()
	app := credentialThrottleApp(2)
	rejecting := app.throttleCredentials(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusUnauthorized, errorBody("invalid_refresh_token", "nope"))
	})
	for attempt := 1; attempt <= 2; attempt++ {
		if recorder := postCredentials(t, rejecting); recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", attempt, recorder.Code)
		}
	}
	// The budget is spent, so even a request that would have succeeded is
	// turned away.
	accepting := app.throttleCredentials(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	})
	if recorder := postCredentials(t, accepting); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", recorder.Code)
	}

	fresh := credentialThrottleApp(2)
	succeeding := fresh.throttleCredentials(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "yes"})
	})
	for attempt := 1; attempt <= 3; attempt++ {
		if recorder := postCredentials(t, succeeding); recorder.Code != http.StatusOK {
			t.Fatalf("successful attempt %d was charged to the failure budget: status=%d", attempt, recorder.Code)
		}
	}
}
