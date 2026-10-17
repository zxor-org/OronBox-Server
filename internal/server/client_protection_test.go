package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

func TestClientProtectionHasNarrowDeliveryExceptions(t *testing.T) {
	a := &App{cfg: config.Config{ClientAttestationEnabled: true}}
	reached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := a.protectClientAPI(a.protectClientRoute(reached))
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/v1/tags", 401}, {"POST", "/api/v1/oauth/bandbbs/start", 401},
		{"GET", "/api/v1/creator/resources/x/blobs/hash", 401},
		{"GET", "/api/v1/blobs/hash", 204}, {"POST", "/api/v1/blobs/hash", 401},
		{"GET", "/api/v1/plugins/x/icon", 204}, {"DELETE", "/api/v1/plugins/x/icon", 401},
		{"GET", "/api/resources", 200}, {"POST", "/api/resources", 404}, {"GET", "/api/me", 404}, {"GET", "/api/resources/actual-private-id", 404}, {"GET", "/admin/api/session", 204}, {"GET", "/oauth2/bandbbs/callback", 204},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Errorf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
}

func TestInvalidTagSceneRejectedWithoutDatabase(t *testing.T) {
	a := &App{}
	w := httptest.NewRecorder()
	a.handleResourceAttributes(w, httptest.NewRequest("GET", "/api/v1/tags?scene=typo", nil))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_scene") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestLegacyNoticeHasAuthorAndDetailWithoutExposingRealResources(t *testing.T) {
	a := &App{cfg: config.Config{ClientAttestationEnabled: true}}
	h := a.protectClientAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("legacy notice reached actual API handler") }))
	for _, path := range []string{"/api/resources?kind=watchface&page=9", "/api/resources/", "/api/resources/" + legacyNoticeID} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"owner":"OrPudding"`) || !strings.Contains(w.Body.String(), legacyNoticeID) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
}
