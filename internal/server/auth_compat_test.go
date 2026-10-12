package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLegacySessionRoutesDisabled(t *testing.T) {
	handler := New(Dependencies{})
	for _, path := range []string{"/api/oauth/bandbbs/refresh", "/api/oauth/bandbbs/exchange"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`)))
			if response.Code != http.StatusNotFound {
				t.Fatalf("expected legacy auth route %s to be 404, got %d", path, response.Code)
			}
		})
	}
}

func TestLegacyAnnouncementEndpoints(t *testing.T) {
	handler := New(Dependencies{})

	req := httptest.NewRequest(http.MethodGet, "/api/announcements/unread", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "announcements") || !strings.Contains(body, "服务器正在更新迁移") {
		t.Fatalf("unexpected announcement body: %s", body)
	}

	reqRead := httptest.NewRequest(http.MethodPost, "/api/announcements/read", nil)
	recRead := httptest.NewRecorder()
	handler.ServeHTTP(recRead, reqRead)
	if recRead.Code != http.StatusOK || !strings.Contains(recRead.Body.String(), "read") {
		t.Fatalf("expected 200 read status, got %d %s", recRead.Code, recRead.Body.String())
	}
}
