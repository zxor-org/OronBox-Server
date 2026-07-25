package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPDoesNotExposeQueryInRequestContext(t *testing.T) {
	var path string
	handler := HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		http.Error(w, `{"error":{"code":"bad","message":"no"}}`, http.StatusBadRequest)
	}))
	request := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=secret", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if path != "/oauth/callback" {
		t.Fatalf("path = %q", path)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("request ID missing")
	}
}

func TestCleanRequestIDRejectsUnsafeValues(t *testing.T) {
	if cleanRequestID("valid_123-id") == "" {
		t.Fatal("valid request ID rejected")
	}
	if cleanRequestID("bad\nvalue") != "" {
		t.Fatal("unsafe request ID accepted")
	}
}

func TestErrorSummaryUsesPublicErrorFields(t *testing.T) {
	got := errorSummary([]byte(`{"error":"creator_invalid","message":"unsupported image"}`))
	if got != "creator_invalid: unsupported image" {
		t.Fatalf("summary = %q", got)
	}
}

func TestHTTPRecoversHandlerPanic(t *testing.T) {
	handler := HTTP(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/broken", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
}
