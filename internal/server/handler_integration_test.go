package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// Handler integration tests need a real PostgreSQL (same skip rule as
// internal/store: TEST_DATABASE_URL unset => skip, so `go test ./...`
// stays green without a DB).

func testHandler(t *testing.T) (http.Handler, *store.Store) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping handler integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(s.Close)
	if err := store.Migrate(ctx, s); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	h := New(Dependencies{Config: config.Config{ClientAttestationEnabled: false}, Store: s, StartedAt: time.Now()})
	return h, s
}

func seedHandlerUser(t *testing.T, db *store.Store, bandbbsUID int64, username, role string) (userID, bearer string) {
	t.Helper()
	ctx := context.Background()
	bearer = token(24)
	refresh := token(24)
	ah, rh := sha256.Sum256([]byte(bearer)), sha256.Sum256([]byte(refresh))
	if err := db.Pool.QueryRow(ctx, `INSERT INTO users(bandbbs_uid,username,role) VALUES($1,$2,$3) ON CONFLICT(bandbbs_uid) DO UPDATE SET username=EXCLUDED.username,role=EXCLUDED.role RETURNING id`, bandbbsUID, username, role).Scan(&userID); err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	if _, err := db.Pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, userID); err != nil {
		t.Fatalf("clear sessions: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO sessions(user_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,platform) VALUES($1,$2,$3,now()+interval '1 hour',now()+interval '1 day','test')`, userID, ah[:], rh[:]); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return userID, bearer
}

func callHandler(h http.Handler, method, path, bearer string, body any) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		raw, _ := json.Marshal(body)
		r = httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("response is not JSON (status=%d body=%s): %v", w.Code, w.Body.String(), err)
	}
	return out
}

func TestCreatorResourceLifecycle(t *testing.T) {
	h, db := testHandler(t)
	ownerID, ownerToken := seedHandlerUser(t, db, 92001, "ht-owner", "creator")
	memberID, memberToken := seedHandlerUser(t, db, 92002, "ht-member", "creator")
	_ = memberID
	_ = ownerID

	pkg := fmt.Sprintf("com.example.ht%d", time.Now().UnixNano()%1000000)

	// 1. Non-creator role is rejected (seed a plain user for the 403 path).
	_, plainToken := seedHandlerUser(t, db, 92003, "ht-plain", "user")
	if w := callHandler(h, "POST", "/api/v1/creator/resources", plainToken, map[string]any{"resource_id": pkg, "title": "HT"}); w.Code != 403 {
		t.Fatalf("plain user create: want 403 got %d (%s)", w.Code, w.Body.String())
	}
	// 2. Unauthenticated is rejected.
	if w := callHandler(h, "GET", "/api/v1/creator/resources", "", nil); w.Code != 401 {
		t.Fatalf("anonymous list: want 401 got %d (%s)", w.Code, w.Body.String())
	}
	// 3. Create draft.
	w := callHandler(h, "POST", "/api/v1/creator/resources", ownerToken, map[string]any{"resource_id": pkg, "title": "HandlerTest", "restype": "quick_app"})
	if w.Code != 201 {
		t.Fatalf("create draft: want 201 got %d (%s)", w.Code, w.Body.String())
	}
	// 4. Duplicate package is rejected.
	if w := callHandler(h, "POST", "/api/v1/creator/resources", ownerToken, map[string]any{"resource_id": pkg}); w.Code != 409 {
		t.Fatalf("duplicate create: want 409 got %d (%s)", w.Code, w.Body.String())
	}
	// 5. List contains the new draft.
	w = callHandler(h, "GET", "/api/v1/creator/resources", ownerToken, nil)
	if w.Code != 200 {
		t.Fatalf("list: want 200 got %d (%s)", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), pkg) {
		t.Fatalf("list missing %s: %s", pkg, w.Body.String())
	}
	// 6. Draft detail (owner) + forbidden for outsiders.
	if w := callHandler(h, "GET", "/api/v1/creator/resources/"+pkg+"/draft", ownerToken, nil); w.Code != 200 {
		t.Fatalf("get draft: want 200 got %d (%s)", w.Code, w.Body.String())
	}
	if w := callHandler(h, "GET", "/api/v1/creator/resources/"+pkg+"/draft", memberToken, nil); w.Code != 403 {
		t.Fatalf("outsider draft: want 403 got %d (%s)", w.Code, w.Body.String())
	}
}

func TestTipAndCommentFlow(t *testing.T) {
	h, db := testHandler(t)
	ownerID, ownerToken := seedHandlerUser(t, db, 92101, "tip-owner", "creator")
	_, tipperToken := seedHandlerUser(t, db, 92102, "tip-tipper", "creator")

	pkg := fmt.Sprintf("com.example.tip%d", time.Now().UnixNano()%1000000)
	if w := callHandler(h, "POST", "/api/v1/creator/resources", ownerToken, map[string]any{"resource_id": pkg, "title": "TipRes"}); w.Code != 201 {
		t.Fatalf("create: want 201 got %d (%s)", w.Code, w.Body.String())
	}
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO user_coin_accounts(user_id,balance) VALUES((SELECT id FROM users WHERE bandbbs_uid=92102),100) ON CONFLICT(user_id) DO UPDATE SET balance=100`); err != nil {
		t.Fatalf("fund tipper: %v", err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO user_coin_accounts(user_id,balance) VALUES($1,0) ON CONFLICT(user_id) DO UPDATE SET balance=0`, ownerID); err != nil {
		t.Fatalf("reset owner: %v", err)
	}

	// 1. Invalid amount rejected.
	if w := callHandler(h, "POST", "/api/v1/resources/"+pkg+"/coins", tipperToken, map[string]any{"amount": 0}); w.Code != 400 {
		t.Fatalf("zero tip: want 400 got %d (%s)", w.Code, w.Body.String())
	}
	// 2. Real tip: 10 units => owner +1 (10%).
	w := callHandler(h, "POST", "/api/v1/resources/"+pkg+"/coins", tipperToken, map[string]any{"amount": 10})
	if w.Code != 200 {
		t.Fatalf("tip: want 200 got %d (%s)", w.Code, w.Body.String())
	}
	out := decodeBody(t, w)
	if out["creator_reward"] != float64(1) {
		t.Fatalf("creator_reward: want 1 got %v (%s)", out["creator_reward"], w.Body.String())
	}
	var ownerBal int64
	if err := db.Pool.QueryRow(ctx, `SELECT balance FROM user_coin_accounts WHERE user_id=$1`, ownerID).Scan(&ownerBal); err != nil || ownerBal != 1 {
		t.Fatalf("owner balance: want 1 got %d (err=%v)", ownerBal, err)
	}
	// 3. Owner got a coin.tip_received message.
	w = callHandler(h, "GET", "/api/v1/messages", ownerToken, nil)
	if w.Code != 200 || !contains(w.Body.String(), "coin.tip_received") {
		t.Fatalf("tip message missing: status=%d body=%s", w.Code, w.Body.String())
	}
	// 4. Root comment by tipper notifies owner; reply notifies parent author.
	w = callHandler(h, "POST", "/api/v1/resources/"+pkg+"/comments", tipperToken, map[string]any{"content": "root comment"})
	if w.Code != 201 {
		t.Fatalf("root comment: want 201 got %d (%s)", w.Code, w.Body.String())
	}
	rootID := decodeBody(t, w)["id"].(string)
	w = callHandler(h, "POST", "/api/v1/resources/"+pkg+"/comments", ownerToken, map[string]any{"content": "reply", "parent_id": rootID})
	if w.Code != 201 {
		t.Fatalf("reply: want 201 got %d (%s)", w.Code, w.Body.String())
	}
	w = callHandler(h, "GET", "/api/v1/resources/"+pkg+"/comments", "", nil)
	if w.Code != 200 || !contains(w.Body.String(), rootID) {
		t.Fatalf("list comments: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCollaboratorLifecycle(t *testing.T) {
	h, db := testHandler(t)
	_, ownerToken := seedHandlerUser(t, db, 92201, "cb-owner", "creator")
	memberID, memberToken := seedHandlerUser(t, db, 92202, "cb-member", "creator")

	pkg := fmt.Sprintf("com.example.cb%d", time.Now().UnixNano()%1000000)
	if w := callHandler(h, "POST", "/api/v1/creator/resources", ownerToken, map[string]any{"resource_id": pkg, "title": "CbRes"}); w.Code != 201 {
		t.Fatalf("create: want 201 got %d (%s)", w.Code, w.Body.String())
	}
	// Invite (owner only).
	w := callHandler(h, "POST", "/api/v1/creator/resources/"+pkg+"/collaborators", ownerToken, map[string]any{"username": "cb-member", "role": "collaborator"})
	if w.Code != 201 {
		t.Fatalf("invite: want 201 got %d (%s)", w.Code, w.Body.String())
	}
	// Member cannot invite others (owner-only path).
	if w := callHandler(h, "POST", "/api/v1/creator/resources/"+pkg+"/collaborators", memberToken, map[string]any{"username": "cb-owner"}); w.Code != 403 {
		t.Fatalf("member invite: want 403 got %d (%s)", w.Code, w.Body.String())
	}
	// Accept as member, then member can read the draft.
	if w := callHandler(h, "POST", "/api/v1/creator/collaborations/"+pkg+"/accept", memberToken, map[string]any{}); w.Code != 200 {
		t.Fatalf("accept: want 200 got %d (%s)", w.Code, w.Body.String())
	}
	if w := callHandler(h, "GET", "/api/v1/creator/resources/"+pkg+"/draft", memberToken, nil); w.Code != 200 {
		t.Fatalf("collaborator draft: want 200 got %d (%s)", w.Code, w.Body.String())
	}
	// Owner removes the collaborator.
	if w := callHandler(h, "DELETE", "/api/v1/creator/resources/"+pkg+"/collaborators/"+memberID, ownerToken, nil); w.Code != 200 {
		t.Fatalf("remove: want 200 got %d (%s)", w.Code, w.Body.String())
	}
}

func TestAdminAndWebhookSmoke(t *testing.T) {
	h, db := testHandler(t)
	adminID, _ := seedHandlerUser(t, db, 92301, "ht-admin", "admin")
	ctx := context.Background()
	adminSession := token(24)
	ah := sha256.Sum256([]byte(adminSession))
	if _, err := db.Pool.Exec(ctx, `INSERT INTO admin_sessions(session_hash,user_id,username,expires_at) VALUES($1,$2,'ht-admin',now()+interval '1 hour')`, ah[:], adminID); err != nil {
		t.Fatalf("seed admin session: %v", err)
	}
	// Admin auth without cookie is rejected.
	r := httptest.NewRequest("GET", "/admin/api/users", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("anonymous admin: want 401 got %d (%s)", w.Code, w.Body.String())
	}
	// With cookie the admin contract answers.
	r = httptest.NewRequest("GET", "/admin/api/users", nil)
	r.AddCookie(&http.Cookie{Name: "oronbox_admin", Value: adminSession})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("admin users: want 200 got %d (%s)", w.Code, w.Body.String())
	}
	// The Gitea webhook endpoint is retired: no manual PR ops, the server drives
	// merges directly from the admin decision API.
	r = httptest.NewRequest("POST", "/api/v1/webhooks/gitea", bytes.NewReader([]byte(`{}`)))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("retired webhook: want 404 got %d", w.Code)
	}
}

func TestPluginLifecycleAndAdminReview(t *testing.T) {
	h, db := testHandler(t)
	dev1ID, dev1Token := seedHandlerUser(t, db, 93101, "plugin-dev-1", "user")
	_, dev2Token := seedHandlerUser(t, db, 93102, "plugin-dev-2", "user")
	adminID, _ := seedHandlerUser(t, db, 93103, "plugin-admin", "admin")

	ctx := context.Background()
	adminSession := token(24)
	ah := sha256.Sum256([]byte(adminSession))
	if _, err := db.Pool.Exec(ctx, `INSERT INTO admin_sessions(session_hash,user_id,username,expires_at) VALUES($1,$2,'plugin-admin',now()+interval '1 hour')`, ah[:], adminID); err != nil {
		t.Fatalf("seed admin session: %v", err)
	}

	tempDir := t.TempDir()
	os.Setenv("PLUGIN_PENDING_DIR", tempDir)
	defer os.Unsetenv("PLUGIN_PENDING_DIR")

	manifest := `{
		"api_level": 1,
		"id": "org.zxor.integration.test",
		"name": "Integration Test Plugin",
		"version": "1.0.0",
		"author": "Alice",
		"description": "Integration test plugin description",
		"runtime": "js",
		"entry": "main.js",
		"permissions": ["ui"]
	}`
	pkgData := createTestPluginZip(t, map[string][]byte{
		"manifest.json": []byte(manifest),
		"main.js":       []byte("console.log('test');"),
	})

	// 1. dev1 uploads plugin package
	r := httptest.NewRequest("POST", "/api/v1/plugins", bytes.NewReader(pkgData))
	r.Header.Set("Authorization", "Bearer "+dev1Token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("dev1 upload plugin: want 200 got %d (%s)", w.Code, w.Body.String())
	}
	var uploadResp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &uploadResp)
	if uploadResp["state"] != "pending" {
		t.Fatalf("expected state pending, got %v", uploadResp["state"])
	}

	// 2. dev1 checks their plugins list
	r = httptest.NewRequest("GET", "/api/v1/plugins/my", nil)
	r.Header.Set("Authorization", "Bearer "+dev1Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("dev1 my plugins: want 200 got %d", w.Code)
	}
	var myResp struct {
		Plugins []map[string]any `json:"plugins"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &myResp)
	if len(myResp.Plugins) != 1 || myResp.Plugins[0]["id"] != "org.zxor.integration.test" {
		t.Fatalf("expected 1 plugin in my list, got %d", len(myResp.Plugins))
	}

	// 3. dev2 attempts to upload same plugin ID (should be forbidden: 403)
	r = httptest.NewRequest("POST", "/api/v1/plugins", bytes.NewReader(pkgData))
	r.Header.Set("Authorization", "Bearer "+dev2Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("dev2 upload same plugin: want 403 got %d (%s)", w.Code, w.Body.String())
	}

	// 4. Admin lists plugins
	r = httptest.NewRequest("GET", "/admin/api/plugins", nil)
	r.AddCookie(&http.Cookie{Name: "oronbox_admin", Value: adminSession})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("admin list plugins: want 200 got %d", w.Code)
	}

	// 5. Admin rejects with note
	reqBody, _ := json.Marshal(map[string]any{"action": "reject", "note": "needs license file"})
	r = httptest.NewRequest("POST", "/admin/api/plugins/org.zxor.integration.test/review", bytes.NewReader(reqBody))
	r.AddCookie(&http.Cookie{Name: "oronbox_admin", Value: adminSession})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("admin reject plugin: want 200 got %d (%s)", w.Code, w.Body.String())
	}

	// 6. dev1 checks status: should be rejected with note
	r = httptest.NewRequest("GET", "/api/v1/plugins/my", nil)
	r.Header.Set("Authorization", "Bearer "+dev1Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	_ = json.Unmarshal(w.Body.Bytes(), &myResp)
	if myResp.Plugins[0]["state"] != "rejected" || myResp.Plugins[0]["moderationReason"] != "needs license file" {
		t.Fatalf("expected state rejected with reason, got %v / %v", myResp.Plugins[0]["state"], myResp.Plugins[0]["moderationReason"])
	}

	// 7. dev1 re-uploads: resets state to pending
	r = httptest.NewRequest("POST", "/api/v1/plugins", bytes.NewReader(pkgData))
	r.Header.Set("Authorization", "Bearer "+dev1Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("dev1 re-upload plugin: want 200 got %d", w.Code)
	}

	// 8. Admin approves
	reqBody, _ = json.Marshal(map[string]any{"action": "approve"})
	r = httptest.NewRequest("POST", "/admin/api/plugins/org.zxor.integration.test/review", bytes.NewReader(reqBody))
	r.AddCookie(&http.Cookie{Name: "oronbox_admin", Value: adminSession})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("admin approve plugin: want 200 got %d (%s)", w.Code, w.Body.String())
	}

	// 9. Admin delists and relists
	reqBody, _ = json.Marshal(map[string]any{"action": "delist", "note": "temporary maintenance"})
	r = httptest.NewRequest("POST", "/admin/api/plugins/org.zxor.integration.test/state", bytes.NewReader(reqBody))
	r.AddCookie(&http.Cookie{Name: "oronbox_admin", Value: adminSession})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("admin delist plugin: want 200 got %d (%s)", w.Code, w.Body.String())
	}

	reqBody, _ = json.Marshal(map[string]any{"action": "relist"})
	r = httptest.NewRequest("POST", "/admin/api/plugins/org.zxor.integration.test/state", bytes.NewReader(reqBody))
	r.AddCookie(&http.Cookie{Name: "oronbox_admin", Value: adminSession})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("admin relist plugin: want 200 got %d (%s)", w.Code, w.Body.String())
	}

	// 10. dev1 deletes their plugin
	r = httptest.NewRequest("DELETE", "/api/v1/plugins/org.zxor.integration.test", nil)
	r.Header.Set("Authorization", "Bearer "+dev1Token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("dev1 delete plugin: want 200 got %d (%s)", w.Code, w.Body.String())
	}

	_ = dev1ID
}

