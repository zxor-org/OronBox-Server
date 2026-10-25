package server

import (
	"github.com/zxor-org/OronBox-Server/internal/config"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivityRequiresClientProtection(t *testing.T) {
	a := &App{cfg: config.Config{ClientAttestationEnabled: true}}
	mux := http.NewServeMux()
	a.registerClientRoutes(mux)
	w := httptest.NewRecorder()
	a.protectClientAPI(mux).ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/activity", strings.NewReader(`{"version":"1","platform":"android"}`)))
	if w.Code != 401 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestActivityRejectsInvalidDimensions(t *testing.T) {
	for _, body := range []string{`{}`, `{"version":"1","platform":"unknown"}`, `{"version":"","platform":"android"}`, `{"version":"x\ny","platform":"ios"}`, strings.Repeat("x", 1025)} {
		w := httptest.NewRecorder()
		(&App{}).handleActivity(w, httptest.NewRequest("POST", "/api/v1/activity", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("status=%d", w.Code)
		}
	}
}
