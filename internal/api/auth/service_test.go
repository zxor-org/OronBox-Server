package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

type oauthRoundTripFunc func(*http.Request) (*http.Response, error)

func (f oauthRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFetchBandBBSUserWithTokenXenForoMe(t *testing.T) {
	previous := oauthHTTPClient
	t.Cleanup(func() { oauthHTTPClient = previous })
	oauthHTTPClient = &http.Client{Transport: oauthRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"access_token":"test-access","refresh_token":"test-refresh","expires_in":3600}`
		if r.URL.Path == "/api/me" {
			if r.Header.Get("Authorization") != "Bearer test-access" {
				t.Fatal("profile request missing bearer authorization")
			}
			body = `{"me":{"user_id":123,"username":"tester","avatar_urls":{"l":"https://example.test/avatar-large.png","m":"https://example.test/avatar.png"}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	s := NewService(config.Config{BandBBS: config.EndpointConfig{TokenURL: "https://bandbbs.test/api/oauth2/token", MeURL: "https://bandbbs.test/api/me"}}, nil, nil, nil)
	uid, name, avatar, _, _, expiry, err := s.FetchBandBBSUserWithToken(context.Background(), "test-code", "https://server.test/callback")
	if err != nil {
		t.Fatalf("XenForo /api/me login failed: %v", err)
	}
	if uid != "123" || name != "tester" || avatar != "https://example.test/avatar-large.png" || expiry != 3600 {
		t.Fatalf("profile not preserved: uid=%s name=%s avatar=%s expiry=%d", uid, name, avatar, expiry)
	}
}

func TestBandBBSProfileCompatibilityAndSafeFailures(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		wantError  string
	}{
		{"proxy user envelope", `{"user":{"user_id":123,"username":"tester","avatar_url":"https://example.test/avatar.png"}}`, 200, ""},
		{"no avatar", `{"me":{"user_id":123,"username":"tester","avatar_urls":{}}}`, 200, ""},
		{"missing identity", `{"me":{"username":"tester"},"access_token":"SECRET_ACCESS"}`, 200, "missing identity"},
		{"empty username", `{"me":{"user_id":123,"username":""}}`, 200, "missing identity"},
		{"bad json", `<html>SECRET_ACCESS</html>`, 200, "invalid JSON"},
		{"upstream forbidden", `{"error":"SECRET_ACCESS"}`, 403, "HTTP 403"},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := oauthHTTPClient
			t.Cleanup(func() { oauthHTTPClient = previous })
			oauthHTTPClient = &http.Client{Transport: oauthRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				status, body := 200, `{"access_token":"SECRET_ACCESS","refresh_token":"SECRET_REFRESH","expires_in":3600}`
				if r.URL.Path == "/api/me" {
					status, body = test.status, test.body
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			s := NewService(config.Config{BandBBS: config.EndpointConfig{TokenURL: "https://bandbbs.test/token", MeURL: "https://bandbbs.test/api/me"}}, nil, nil, nil)
			uid, _, _, _, _, _, err := s.FetchBandBBSUserWithToken(context.Background(), "code", "https://server.test/callback")
			if test.wantError == "" {
				if err != nil || uid != "123" {
					t.Fatalf("compatible response failed: uid=%s err=%v", uid, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("expected diagnostic %s, got %v", test.wantError, err)
			}
			if strings.Contains(err.Error(), "SECRET_") {
				t.Fatal("upstream credentials leaked into error")
			}
		})
	}
}
