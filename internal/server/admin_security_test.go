package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

func TestAdminOAuthReturnIsLimitedToDashboardGET(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		method string
		target string
		want   bool
	}{
		{name: "dashboard callback", method: http.MethodGet, target: "/admin?ticket=one-time-ticket", want: true},
		{name: "empty ticket", method: http.MethodGet, target: "/admin?ticket=", want: false},
		{name: "resource read", method: http.MethodGet, target: "/admin/resources?ticket=forged", want: false},
		{name: "resource mutation", method: http.MethodPost, target: "/admin/resources/id/state?ticket=forged", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(tt.method, tt.target, nil)
			if got := isAdminOAuthReturn(request); got != tt.want {
				t.Fatalf("isAdminOAuthReturn() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestAdminMutationRequiresConfiguredOrigin(t *testing.T) {
	t.Parallel()
	app := &App{cfg: config.Config{PublicURL: "https://ob-api.zxor.org"}}
	tests := []struct {
		name    string
		origin  string
		referer string
		want    bool
	}{
		{name: "same origin", origin: "https://ob-api.zxor.org", want: true},
		{name: "same origin referer", referer: "https://ob-api.zxor.org/admin/resources/1", want: true},
		{name: "wrong scheme", origin: "http://ob-api.zxor.org", want: false},
		{name: "wrong host", origin: "https://example.com", want: false},
		{name: "malicious host suffix", origin: "https://ob-api.zxor.org.example.com", want: false},
		{name: "missing browser origin", want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodPost, "/admin/resources/id/state", nil)
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				request.Header.Set("Referer", tt.referer)
			}
			if got := app.isSameOriginAdminRequest(request); got != tt.want {
				t.Fatalf("isSameOriginAdminRequest() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestSecureAdminCookieUsesPublicURLBehindProxy(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/admin", nil)
	if !(&App{cfg: config.Config{PublicURL: "https://ob-api.zxor.org"}}).secureAdminCookie(request) {
		t.Fatal("HTTPS PUBLIC_URL must produce a Secure cookie even when the upstream request has no TLS state")
	}
	if (&App{cfg: config.Config{PublicURL: "http://localhost:8080"}}).secureAdminCookie(request) {
		t.Fatal("plain HTTP development URL unexpectedly produced a Secure cookie")
	}
}

func TestAdminSessionCookieSurvivesOAuthRedirectChain(t *testing.T) {
	t.Parallel()
	app := &App{cfg: config.Config{PublicURL: "https://ob-api.zxor.org"}}
	request := httptest.NewRequest(http.MethodGet, "/admin?ticket=oauth-ticket", nil)
	expiresAt := time.Now().UTC().Add(12 * time.Hour)

	cookie := app.adminSessionCookie(request, "session-id", expiresAt, 0)
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("admin cookie SameSite = %v, want Lax for the cross-site OAuth redirect chain", cookie.SameSite)
	}
	if !cookie.Secure || !cookie.HttpOnly || cookie.Path != "/admin" {
		t.Fatalf("admin cookie lost security attributes: %#v", cookie)
	}
}

func TestClientIPOnlyTrustsForwardingHeadersFromConfiguredProxy(t *testing.T) {
	t.Parallel()
	_, trustedProxy, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	app := &App{trustedProxies: []*net.IPNet{trustedProxy}}

	untrusted := httptest.NewRequest(http.MethodGet, "/", nil)
	untrusted.RemoteAddr = "203.0.113.8:49152"
	untrusted.Header.Set("CF-Connecting-IP", "198.51.100.7")
	if got := app.clientIP(untrusted); got != "203.0.113.8" {
		t.Fatalf("untrusted proxy spoofed client IP: got %q", got)
	}

	trusted := httptest.NewRequest(http.MethodGet, "/", nil)
	trusted.RemoteAddr = "10.1.2.3:49152"
	trusted.Header.Set("CF-Connecting-IP", "198.51.100.7")
	if got := app.clientIP(trusted); got != "198.51.100.7" {
		t.Fatalf("trusted proxy client IP = %q, want %q", got, "198.51.100.7")
	}

	invalid := httptest.NewRequest(http.MethodGet, "/", nil)
	invalid.RemoteAddr = "10.1.2.3:49152"
	invalid.Header.Set("CF-Connecting-IP", "not-an-ip")
	invalid.Header.Set("X-Forwarded-For", "also-invalid, 192.0.2.9")
	if got := app.clientIP(invalid); got != "192.0.2.9" {
		t.Fatalf("valid fallback forwarded IP = %q, want %q", got, "192.0.2.9")
	}
}

func TestInternalErrorsDoNotLeakHandlerDetails(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Request-ID", "test-request")
	writeJSON(
		recorder,
		http.StatusInternalServerError,
		errorBody("database_failed", `password="secret" host=private.example`),
	)

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "database_failed" {
		t.Fatalf("error code = %q", body["error"])
	}
	if body["message"] != "The server could not complete the request" {
		t.Fatalf("unexpected public message %q", body["message"])
	}
}

func TestAdminSettingsJSONDoesNotLeakOAuthSecrets(t *testing.T) {
	t.Parallel()
	app := &App{
		cfg: config.Config{
			BandBBS: config.BandBBSConfig{ClientID: "bandbbs-id", ClientSecret: "bandbbs-secret"},
			GitHub:  config.GitHubConfig{ClientID: "github-id", ClientSecret: "github-secret"},
		},
	}
	recorder := httptest.NewRecorder()
	app.handleAdminAPISettings(recorder, httptest.NewRequest(http.MethodGet, "/admin/api/settings", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, secret := range []string{"bandbbs-secret", "github-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("settings JSON leaked OAuth secret %q", secret)
		}
	}
	for _, clientID := range []string{"bandbbs-id", "github-id"} {
		if !strings.Contains(body, clientID) {
			t.Fatalf("settings JSON is missing client ID %q", clientID)
		}
	}
}

func TestAdminAPIRequiresLogin(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	(&App{cfg: config.Config{PublicURL: "https://admin.example"}}).Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/admin/api/session", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "unauthenticated") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}
