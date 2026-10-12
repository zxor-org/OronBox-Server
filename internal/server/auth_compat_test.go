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

func TestLegacyResourcesFallback(t *testing.T) {
	handler := New(Dependencies{})

	reqRes := httptest.NewRequest(http.MethodGet, "/api/resources", nil)
	recRes := httptest.NewRecorder()
	handler.ServeHTTP(recRes, reqRes)
	if recRes.Code != http.StatusOK || !strings.Contains(recRes.Body.String(), "migration-maintenance-notice") {
		t.Fatalf("expected legacy resources shell notice, got %d %s", recRes.Code, recRes.Body.String())
	}
}
