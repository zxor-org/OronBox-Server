package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

func TestPublicRoutesUseContractShapes(t *testing.T) {
	cfg := config.Config{ClientAttestationEnabled: false}
	h := New(Dependencies{Config: cfg, StartedAt: time.Now()})
	for _, tt := range []struct{ path, want string }{{"/healthz", "pass"}, {"/api/v1/notices", "notices"}} {
		r := httptest.NewRequest("GET", tt.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("%s status=%d", tt.path, w.Code)
		}
		if !contains(w.Body.String(), tt.want) {
			t.Errorf("%s missing %q: %s", tt.path, tt.want, w.Body.String())
		}
	}
	// No release cache in this test: spec requires 404 release_not_found.
	r := httptest.NewRequest("GET", "/api/v1/app/releases", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 || !contains(w.Body.String(), "release_not_found") {
		t.Fatalf("releases empty-cache status=%d body=%s", w.Code, w.Body.String())
	}
}
func TestPublicCommentsDoNotRequireBearer(t *testing.T) {
	h := New(Dependencies{Config: config.Config{ClientAttestationEnabled: false}, StartedAt: time.Now()})
	r := httptest.NewRequest("GET", "/api/v1/resources/com.example/comments", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestTransitionPagesAndAssets(t *testing.T) {
	h := New(Dependencies{Config: config.Config{ClientAttestationEnabled: false}, StartedAt: time.Now()})

	cases := []struct {
		name        string
		path        string
		wantCode    int
		contentType string
		wantBody    string
	}{
		{"CSS asset", "/assets/app.css", 200, "text/css; charset=utf-8", ":root"},
		{"Theme JS asset", "/assets/theme.js", 200, "text/javascript; charset=utf-8", "oronbox_server_theme"},
		{"Transition JS asset", "/assets/transition.js", 200, "text/javascript; charset=utf-8", "data-transition-target"},
		{"Favicon SVG asset", "/assets/favicon.svg", 200, "image/svg+xml", "<svg"},
		{"Favicon ICO fallback", "/favicon.ico", 200, "image/svg+xml", "<svg"},
		{"Server Home Redirect", "/", 302, "", ""},
		{"Auth success page", "/auth/success", 200, "text/html; charset=utf-8", "授权完成"},
		{"Auth failed page", "/auth/failed?error=invalid_callback", 200, "text/html; charset=utf-8", "授权回调缺少必要信息"},
		{"Open res transition page", "/open?res=com.test.watchface", 200, "text/html; charset=utf-8", "oronbox://open?res=com.test.watchface"},
		{"Open deviceQr transition page", "/open?source=deviceQr&name=Band8&mac=001122334455&authkey=mykey", 200, "text/html; charset=utf-8", "oronbox://open?"},
		{"Open invalid redirects home", "/open", 302, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.wantCode {
				t.Fatalf("%s: expected status %d, got %d", tc.path, tc.wantCode, w.Code)
			}
			if tc.contentType != "" {
				gotCT := w.Header().Get("Content-Type")
				if gotCT != tc.contentType {
					t.Errorf("%s: expected Content-Type %q, got %q", tc.path, tc.contentType, gotCT)
				}
			}
			if tc.wantBody != "" && !contains(w.Body.String(), tc.wantBody) {
				t.Errorf("%s: response body does not contain %q\nBody:\n%s", tc.path, tc.wantBody, w.Body.String())
			}
		})
	}
}

func TestAdminAuthorizeUsesUserReadScope(t *testing.T) {
	cfg := config.Config{
		ClientAttestationEnabled: false,
		BandBBS: config.EndpointConfig{
			AuthorizeURL: "https://www.bandbbs.cn/oauth2/authorize",
			ClientID:     "my-id",
			RedirectURI:  "https://ob-api.zxor.org/oauth2/bandbbs/callback",
		},
	}
	h := New(Dependencies{Config: cfg, StartedAt: time.Now()})
	r := httptest.NewRequest("GET", "/admin/api/auth/bandbbs/authorize", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 302 {
		t.Fatalf("expected 302 redirect, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !contains(loc, "scope=user%3Aread") && !contains(loc, "scope=user:read") {
		t.Fatalf("expected scope=user:read in redirect url, got %s", loc)
	}
}

func TestAdminSessionAndLogoutUnauthenticated(t *testing.T) {
	cfg := config.Config{ClientAttestationEnabled: false}
	h := New(Dependencies{Config: cfg, StartedAt: time.Now()})

	// Test GET /admin/api/auth/session with no cookie
	r := httptest.NewRequest("GET", "/admin/api/auth/session", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !contains(w.Body.String(), `"authenticated":false`) {
		t.Fatalf("expected authenticated:false, got code=%d body=%s", w.Code, w.Body.String())
	}

	// Test POST /admin/api/auth/logout clears cookie
	r = httptest.NewRequest("POST", "/admin/api/auth/logout", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	cookie := w.Header().Get("Set-Cookie")
	if !contains(cookie, "oronbox_admin=") || !contains(cookie, "Max-Age=0") {
		t.Fatalf("expected cookie clearing, got Set-Cookie: %s", cookie)
	}

	// Test protected admin endpoint with no cookie
	r = httptest.NewRequest("GET", "/admin/api/revocations", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || !contains(w.Body.String(), "admin_unauthorized") {
		t.Fatalf("expected 401 admin_unauthorized, got code=%d body=%s", w.Code, w.Body.String())
	}

	// Test POST /admin/api/auth/refresh with no cookie
	r = httptest.NewRequest("POST", "/admin/api/auth/refresh", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || !contains(w.Body.String(), "admin_unauthorized") {
		t.Fatalf("expected 401 admin_unauthorized on refresh, got code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRevokeBandBBSToken(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if e := r.ParseForm(); e != nil {
			t.Fatal(e)
		}
		if r.Form.Get("token") != "test-token" || r.Form.Get("token_type_hint") != "access_token" {
			t.Errorf("unexpected form: %v", r.Form)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	app := &application{
		cfg: config.Config{
			BandBBS: config.EndpointConfig{
				ClientID:     "cid",
				ClientSecret: "csec",
				RevokeURL:    server.URL,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := app.revokeBandBBSToken(ctx, "test-token"); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	if !called {
		t.Fatalf("expected revoke endpoint to be called")
	}
}

func TestConsoleRouteRedirectAndUnauthenticatedLogin(t *testing.T) {
	cfg := config.Config{ClientAttestationEnabled: false}
	h := New(Dependencies{Config: cfg, StartedAt: time.Now()})

	// 1. GET /console redirects to /console/ (301)
	r := httptest.NewRequest("GET", "/console", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("expected 301 redirect, got %d", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/console/" {
		t.Fatalf("expected Location /console/, got %s", loc)
	}

	// 2. GET /console/ without cookie renders built-in admin_login page
	r = httptest.NewRequest("GET", "/console/", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}
	body := w.Body.String()
	if !contains(body, "OronBox 管理后台") {
		t.Fatalf("expected admin login title in body, got: %s", body)
	}
	if !contains(body, "/admin/api/auth/bandbbs/authorize") {
		t.Fatalf("expected authorize url in body, got: %s", body)
	}
	if !contains(body, "i-admin_panel_settings") {
		t.Fatalf("expected admin shield icon in body, got: %s", body)
	}
}

func TestConsoleServingAndSPAFallback(t *testing.T) {
	// Create a temporary console directory
	tmpDir, err := os.MkdirTemp("", "console_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Write index.html
	indexHTML := `<!doctype html><html><body><h1>OronBox Console SPA</h1></body></html>`
	if err := os.WriteFile(filepath.Join(tmpDir, "index.html"), []byte(indexHTML), 0644); err != nil {
		t.Fatal(err)
	}

	// Write an asset file
	if err := os.MkdirAll(filepath.Join(tmpDir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	assetJS := `console.log("hello");`
	if err := os.WriteFile(filepath.Join(tmpDir, "assets", "app.js"), []byte(assetJS), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a hidden metadata file
	if err := os.WriteFile(filepath.Join(tmpDir, ".console_info"), []byte("secret_commit_hash"), 0644); err != nil {
		t.Fatal(err)
	}

	app := &application{
		cfg: config.Config{
			ConsoleDir: tmpDir,
		},
		adminAuthFn: func(r *http.Request) bool { return true },
	}

	// 1. Serving index.html
	r1 := httptest.NewRequest("GET", "/console/", nil)
	w1 := httptest.NewRecorder()
	app.consoleHandler(w1, r1)
	if w1.Code != 200 || !contains(w1.Body.String(), "OronBox Console SPA") {
		t.Fatalf("unexpected index.html response: code=%d body=%s", w1.Code, w1.Body.String())
	}

	// 2. Serving static assets
	r2 := httptest.NewRequest("GET", "/console/assets/app.js", nil)
	w2 := httptest.NewRecorder()
	app.consoleHandler(w2, r2)
	if w2.Code != 200 || !contains(w2.Body.String(), "hello") {
		t.Fatalf("unexpected asset response: code=%d body=%s", w2.Code, w2.Body.String())
	}

	// 3. Security: hidden file (.console_info) must NOT be leaked
	r3 := httptest.NewRequest("GET", "/console/.console_info", nil)
	w3 := httptest.NewRecorder()
	app.consoleHandler(w3, r3)
	if contains(w3.Body.String(), "secret_commit_hash") {
		t.Fatalf("hidden file leaked! body=%s", w3.Body.String())
	}
	if !contains(w3.Body.String(), "OronBox Console SPA") {
		t.Fatalf("expected SPA fallback for hidden file: code=%d body=%s", w3.Code, w3.Body.String())
	}

	// 4. Security: path traversal attempt must be confined and never leak files
	r4 := httptest.NewRequest("GET", "/console/../../../../etc/passwd", nil)
	w4 := httptest.NewRecorder()
	app.consoleHandler(w4, r4)
	if contains(w4.Body.String(), "root:") {
		t.Fatalf("path traversal leaked file! body=%s", w4.Body.String())
	}
	if w4.Code != 200 && w4.Code != 400 {
		t.Fatalf("unexpected code on path traversal: code=%d", w4.Code)
	}

	// 5. Test 404 behavior when index does not exist in an empty dir
	emptyDir, _ := os.MkdirTemp("", "console_empty_*")
	defer os.RemoveAll(emptyDir)
	appEmpty := &application{
		cfg: config.Config{
			ConsoleDir: emptyDir,
		},
		adminAuthFn: func(r *http.Request) bool { return true },
	}
	r5 := httptest.NewRequest("GET", "/console/", nil)
	w5 := httptest.NewRecorder()
	appEmpty.consoleHandler(w5, r5)
	if w5.Code != 404 {
		t.Fatalf("expected 404 on unconfigured empty console dir: code=%d", w5.Code)
	}
}

func TestUntarArchiveAndSyncConsole(t *testing.T) {
	// 1. Create a dummy tar.gz archive in memory containing dist/index.html and assets/main.js
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	files := []struct {
		name string
		body string
	}{
		{"dist/index.html", "<!doctype html><html><head><title>Console</title></head><body>OK</body></html>"},
		{"dist/assets/main.js", `console.log("main loaded");`},
	}

	for _, f := range files {
		hdr := &tar.Header{
			Name: f.name,
			Mode: 0644,
			Size: int64(len(f.body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gw.Close()

	tarGzBytes := buf.Bytes()

	// 2. Mock Gitea release and asset download server
	mockGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			// Return release json pointing to the download URL
			downloadURL := "http://" + r.Host + "/attachments/console.tar.gz"
			jsonPayload := fmt.Sprintf(`{
				"id": 101,
				"tag_name": "latest",
				"target_commitish": "abc1234",
				"published_at": "2026-10-06T20:00:00Z",
				"assets": [
					{
						"id": 202,
						"name": "console.tar.gz",
						"size": %d,
						"browser_download_url": %q
					}
				]
			}`, len(tarGzBytes), downloadURL)
			w.Write([]byte(jsonPayload))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/attachments/console.tar.gz") {
			w.Header().Set("Content-Type", "application/gzip")
			w.Write(tarGzBytes)
			return
		}
		w.WriteHeader(404)
	}))
	defer mockGitea.Close()

	targetConsoleDir, err := os.MkdirTemp("", "console_dest_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(targetConsoleDir)

	app := &application{
		cfg: config.Config{
			ConsoleDir: targetConsoleDir,
			Gitea: config.EndpointConfig{
				APIURL:       mockGitea.URL,
				ClientSecret: "test-bot-token",
				ConsoleRepo:  "OronBoxCommunity/OronBox-Server-Console",
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := app.syncConsole(ctx)
	if err != nil {
		t.Fatalf("syncConsole failed: %v", err)
	}
	if !res.Synced {
		t.Fatalf("expected synced=true, got false")
	}

	// Verify index.html was extracted and promoted from dist/ to root
	indexContent, err := os.ReadFile(filepath.Join(targetConsoleDir, "index.html"))
	if err != nil {
		t.Fatalf("index.html not found: %v", err)
	}
	if !contains(string(indexContent), "Console") {
		t.Fatalf("unexpected index.html content: %s", string(indexContent))
	}

	// Verify assets/main.js was extracted
	jsContent, err := os.ReadFile(filepath.Join(targetConsoleDir, "assets", "main.js"))
	if err != nil {
		t.Fatalf("assets/main.js not found: %v", err)
	}
	if !contains(string(jsContent), "main loaded") {
		t.Fatalf("unexpected main.js content: %s", string(jsContent))
	}

	// Verify .console_info
	infoContent, err := os.ReadFile(filepath.Join(targetConsoleDir, ".console_info"))
	if err != nil {
		t.Fatalf(".console_info not found: %v", err)
	}
	if !contains(string(infoContent), "abc1234") {
		t.Fatalf("unexpected .console_info content: %s", string(infoContent))
	}

	// 3. Test consoleStatusGet returns installed=true and current metadata
	statusReq := httptest.NewRequest("GET", "/admin/api/console/status", nil)
	statusRec := httptest.NewRecorder()
	app.consoleStatusGet(statusRec, statusReq)
	if statusRec.Code != 200 {
		t.Fatalf("status code: %d", statusRec.Code)
	}
	statusBody := statusRec.Body.String()
	if !contains(statusBody, `"installed":true`) || !contains(statusBody, "abc1234") {
		t.Fatalf("unexpected status body: %s", statusBody)
	}

	// 4. Test consoleSyncPost when syncing is in progress returns sync_already_in_progress
	globalConsoleState.mu.Lock()
	globalConsoleState.Syncing = true
	globalConsoleState.mu.Unlock()

	syncReq := httptest.NewRequest("POST", "/admin/api/console/sync", nil)
	syncRec := httptest.NewRecorder()
	app.consoleSyncPost(syncRec, syncReq)
	if syncRec.Code != 200 || !contains(syncRec.Body.String(), "sync_already_in_progress") {
		t.Fatalf("expected sync_already_in_progress, got code=%d body=%s", syncRec.Code, syncRec.Body.String())
	}

	globalConsoleState.mu.Lock()
	globalConsoleState.Syncing = false
	globalConsoleState.mu.Unlock()
}

func TestConsoleFailedSyncRetainsPreviousVersionAndStatus(t *testing.T) {
	globalConsoleState.mu.Lock()
	globalConsoleState.Syncing = false
	globalConsoleState.LastError = ""
	globalConsoleState.Current = nil
	globalConsoleState.Latest = nil
	globalConsoleState.mu.Unlock()

	targetConsoleDir, err := os.MkdirTemp("", "console_fail_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(targetConsoleDir)
	defer os.RemoveAll(targetConsoleDir + "_prev")

	// 1. Prepare existing version in targetConsoleDir
	origIndex := "<html><body>Old Console Version 1</body></html>"
	if err := os.WriteFile(filepath.Join(targetConsoleDir, "index.html"), []byte(origIndex), 0644); err != nil {
		t.Fatal(err)
	}
	origMeta := &consoleMetadata{Commit: "v1_commit", PublishedAt: "2026-01-01T00:00:00Z", AssetID: 101}
	mBytes, _ := json.Marshal(origMeta)
	_ = os.WriteFile(filepath.Join(targetConsoleDir, ".console_info"), mBytes, 0644)

	// Mock Gitea that returns an error on download
	mockGitea := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"target_commitish": "v2_commit",
				"published_at": "2026-01-02T00:00:00Z",
				"assets": [{"id": 202, "name": "console.tar.gz", "browser_download_url": "http://example.com/fail.tar.gz"}]
			}`))
			return
		}
		w.WriteHeader(500)
	}))
	defer mockGitea.Close()

	app := &application{
		cfg: config.Config{
			ConsoleDir: targetConsoleDir,
			Gitea: config.EndpointConfig{
				APIURL:       mockGitea.URL,
				ClientSecret: "test-bot-token",
				ConsoleRepo:  "OronBoxCommunity/OronBox-Server-Console",
			},
		},
	}

	// 2. Perform sync (it will fail because download fails)
	_, syncErr := app.syncConsole(context.Background())
	if syncErr == nil {
		t.Fatalf("expected syncConsole to fail, but it succeeded")
	}

	// 3. Verify existing version is retained (not corrupted or overwritten)
	retainedContent, err := os.ReadFile(filepath.Join(targetConsoleDir, "index.html"))
	if err != nil {
		t.Fatalf("index.html should still exist: %v", err)
	}
	if string(retainedContent) != origIndex {
		t.Fatalf("index.html was modified! got: %s", string(retainedContent))
	}

	// 4. Verify consoleHandler first renders failure card with '返回管理后台' button
	app.adminAuthFn = func(r *http.Request) bool { return true }
	req := httptest.NewRequest("GET", "/console/", nil)
	rec := httptest.NewRecorder()
	app.consoleHandler(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !contains(rec.Body.String(), "前端同步失败") {
		t.Fatalf("consoleHandler should show failure card, got: %s", rec.Body.String())
	}
	if !contains(rec.Body.String(), "返回管理后台") || !contains(rec.Body.String(), "/console/?dismiss=1") {
		t.Fatalf("consoleHandler failure card should have '返回管理后台' button, got: %s", rec.Body.String())
	}

	// 5. Verify clicking '返回管理后台' (/console/?dismiss=1) enters the existing old frontend
	reqDismiss := httptest.NewRequest("GET", "/console/?dismiss=1", nil)
	recDismiss := httptest.NewRecorder()
	app.consoleHandler(recDismiss, reqDismiss)

	if recDismiss.Code != 200 {
		t.Fatalf("expected status 200, got %d", recDismiss.Code)
	}
	if !contains(recDismiss.Body.String(), "Old Console Version 1") {
		t.Fatalf("expected old frontend version after dismiss, got: %s", recDismiss.Body.String())
	}

	// 6. Verify consoleStatusGet exposes last_error so old frontend can display status and retry button
	statusReq := httptest.NewRequest("GET", "/admin/api/console/status", nil)
	statusRec := httptest.NewRecorder()
	app.consoleStatusGet(statusRec, statusReq)
	if statusRec.Code != 200 {
		t.Fatalf("status code: %d", statusRec.Code)
	}
	statusBody := statusRec.Body.String()
	if !contains(statusBody, `"installed":true`) {
		t.Fatalf("expected installed=true, got: %s", statusBody)
	}
	if !contains(statusBody, "last_error") {
		t.Fatalf("expected last_error in response, got: %s", statusBody)
	}
}

func TestUntarArchiveRejectsOversizedFiles(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Create header with size exceeding maxTarFileSize
	header := &tar.Header{
		Name:     "dist/huge.bin",
		Size:     maxTarFileSize + 1024,
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gw.Close()

	destDir, err := os.MkdirTemp("", "tar_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(destDir)

	err = untarArchive(&buf, destDir)
	if err == nil {
		t.Fatalf("expected untarArchive to reject oversized file, but succeeded")
	}
	if !contains(err.Error(), "exceeds max size") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestParseBandBBSProfile(t *testing.T) {
	t.Run("Standard nested me object", func(t *testing.T) {
		jsonStr := `{"me": {"user_id": 12345, "username": "test_user", "avatar_urls": {"o": "https://avatar.example/o.png", "m": "https://avatar.example/m.png"}}}`
		uid, username, avatar, err := parseBandBBSProfile(strings.NewReader(jsonStr))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if uid != "12345" {
			t.Errorf("expected uid=12345, got %q", uid)
		}
		if username != "test_user" {
			t.Errorf("expected username=test_user, got %q", username)
		}
		if avatar != "https://avatar.example/o.png" {
			t.Errorf("expected avatar, got %q", avatar)
		}
	})

	t.Run("Flat user_id and string avatar", func(t *testing.T) {
		jsonStr := `{"user_id": 12345, "username": "test_user", "avatar_url": "https://avatar.example/a.png"}`
		uid, username, avatar, err := parseBandBBSProfile(strings.NewReader(jsonStr))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if uid != "12345" || username != "test_user" || avatar != "https://avatar.example/a.png" {
			t.Errorf("unexpected parse result: %s, %s, %s", uid, username, avatar)
		}
	})

	t.Run("Missing user_id fails", func(t *testing.T) {
		jsonStr := `{"username": "anonymous"}`
		_, _, _, err := parseBandBBSProfile(strings.NewReader(jsonStr))
		if err == nil {
			t.Fatalf("expected error for missing user_id, got nil")
		}
	})
}



