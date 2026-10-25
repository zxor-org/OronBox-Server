package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func consoleAuthDatabase(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := store.Open(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := "test_console_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(ctx, `DROP DATABASE `+name); err != nil {
			t.Error(err)
		}
	})
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}
func TestConsoleOAuthIsolationAndSessionRotation(t *testing.T) {
	db := consoleAuthDatabase(t)
	ctx := context.Background()
	s := store.New(db)
	uid := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO users(id,bandbbs_user_id,username,role)VALUES($1,123,'console-admin','admin')`, uid); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			fmt.Fprint(w, `{"access_token":"console-read","refresh_token":"console-refresh","token_type":"Bearer","expires_in":3600,"scope":"user:read"}`)
		case "/introspect":
			fmt.Fprint(w, `{"active":true,"scope":"user:read","sub":"123"}`)
		case "/me":
			fmt.Fprint(w, `{"me":{"user_id":123,"username":"console-admin"}}`)
		case "/revoke":
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	cfg := config.Config{PublicURL: "https://console.example", ClientRedirectURI: "oronbox://oauth/bandbbs", SessionSecret: "console-test-secret", EncryptionKey: "console-test-encryption", StateTTL: time.Minute, LoginTicketTTL: time.Minute, AccessTokenTTL: time.Hour, RefreshTokenTTL: time.Hour * 24}
	cfg.BandBBS = config.BandBBSConfig{AuthorizeURL: upstream.URL + "/authorize", TokenURL: upstream.URL + "/token", MeURL: upstream.URL + "/me", IntrospectURL: upstream.URL + "/introspect", RevokeURL: upstream.URL + "/revoke", RedirectURI: "https://console.example/oauth2/bandbbs/callback", Scopes: config.ParseScopes(config.DefaultScopes)}
	app := New(Dependencies{Config: cfg, Store: s, BandBBS: bandbbs.NewClient(cfg.BandBBS, time.Second)})
	if err := s.UpsertOAuthGrant(ctx, store.GrantParams{UserID: uid, Provider: ProviderBandBBS, Subject: "123", Scopes: config.ParseScopes(config.DefaultScopes), AccessTokenCipher: []byte("existing-client-grant"), TokenType: "Bearer"}); err != nil {
		t.Fatal(err)
	}
	handler := app.Routes()
	request := func(method, path string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if method == "POST" {
			req.Header.Set("Origin", cfg.PublicURL)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	start := func() (string, *http.Cookie) {
		w := request("GET", "/console/api/auth/bandbbs/authorize", nil, "")
		if w.Code != 302 {
			t.Fatalf("authorize: %d %s", w.Code, w.Body.String())
		}
		u, err := url.Parse(w.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		if q.Get("scope") != "user:read" || q.Get("redirect_uri") != cfg.BandBBS.RedirectURI || !strings.HasPrefix(q.Get("state"), "admin_") {
			t.Fatalf("authorization query=%v", q)
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].Path != "/oauth2/bandbbs/callback" {
			t.Fatalf("binding cookie=%v", cookies)
		}
		return q.Get("state"), cookies[0]
	}
	state, _ := start()
	w := request("GET", "/oauth2/bandbbs/callback?state="+state+"&code=code", nil, "")
	if !strings.Contains(w.Header().Get("Location"), "invalid_state") {
		t.Fatalf("unbound callback: %d %s", w.Code, w.Body.String())
	}
	state, binding := start()
	w = request("GET", "/oauth2/bandbbs/callback?state="+state+"&code=code", binding, "")
	if w.Code != 303 || w.Header().Get("Location") != "/console/" {
		t.Fatalf("callback: %d %s %s", w.Code, w.Header(), w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == adminCookieName {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("missing admin cookie")
	}
	for _, table := range []string{"oauth_grants", "login_tickets", "sessions"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || (table == "oauth_grants" && n != 1) || (table != "oauth_grants" && n != 0) {
			t.Fatalf("unexpected %s records %d err=%v", table, n, err)
		}
	}
	var keptGrant []byte
	if err := db.QueryRow(`SELECT access_token_cipher FROM oauth_grants WHERE user_id=$1 AND provider='bandbbs'`, uid).Scan(&keptGrant); err != nil || string(keptGrant) != "existing-client-grant" {
		t.Fatal("console login changed the client grant", err)
	}
	var storedID string
	if err := db.QueryRow(`SELECT id FROM admin_sessions`).Scan(&storedID); err != nil || storedID == cookie.Value || storedID != store.AdminCredentialHash(cookie.Value) {
		t.Fatal("credential was not hashed", err)
	}
	type receipt struct {
		CSRF     string    `json:"csrf_token"`
		Expires  time.Time `json:"expires_at"`
		Absolute time.Time `json:"absolute_expires_at"`
	}
	readSession := func(c *http.Cookie) receipt {
		w := request("GET", "/console/api/auth/session", c, "")
		if w.Code != 200 {
			t.Fatalf("session: %d %s", w.Code, w.Body.String())
		}
		var out receipt
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, err := db.Exec(`UPDATE admin_sessions SET expires_at=now()+interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	initial := readSession(cookie)
	if initial.Expires.Before(time.Now().Add(11 * time.Hour)) {
		t.Fatal("authenticated activity did not extend idle lifetime")
	}

	if w := request("POST", "/console/api/auth/refresh", cookie, ""); w.Code != 403 {
		t.Fatalf("refresh without csrf=%d", w.Code)
	}
	const count = 8
	outputs := make(chan *httptest.ResponseRecorder, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); outputs <- request("POST", "/console/api/auth/refresh", cookie, initial.CSRF) }()
	}
	wg.Wait()
	close(outputs)
	var rotated *http.Cookie
	for out := range outputs {
		if out.Code != 200 {
			t.Fatalf("rotation: %d %s", out.Code, out.Body.String())
		}
		cs := out.Result().Cookies()
		if len(cs) != 1 {
			t.Fatalf("rotation cookies=%v", cs)
		}
		if rotated != nil && rotated.Value != cs[0].Value {
			t.Fatal("concurrent refresh rotated multiple times")
		}
		rotated = cs[0]
	}
	if rotated.Value == cookie.Value {
		t.Fatal("credential did not rotate")
	}
	next := readSession(rotated)
	if next.CSRF != initial.CSRF {
		t.Fatal("csrf changed during credential rotation")
	}
	if w := request("GET", "/console/api/auth/session", cookie, ""); w.Code != 200 {
		t.Fatal("overlap rejected old credential")
	}
	if _, err := db.Exec(`UPDATE admin_sessions SET previous_until=now()-interval '1 second',rotated_at=now()-interval '31 seconds',absolute_expires_at=now()+interval '1 hour',expires_at=now()+interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/console/api/auth/session", cookie, ""); w.Code != 401 {
		t.Fatalf("expired overlap=%d", w.Code)
	}
	w = request("POST", "/console/api/auth/refresh", rotated, initial.CSRF)
	if w.Code != 200 {
		t.Fatalf("second refresh=%d %s", w.Code, w.Body.String())
	}
	rotated = w.Result().Cookies()[0]
	cap := readSession(rotated)
	if cap.Expires.After(cap.Absolute) || cap.Absolute.After(time.Now().Add(time.Hour+time.Second)) {
		t.Fatal("refresh exceeded absolute cap")
	}
	if _, err := db.Exec(`UPDATE users SET role='user' WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/console/api/auth/session", rotated, ""); w.Code != 403 {
		t.Fatalf("role revocation=%d", w.Code)
	}
	if _, err := db.Exec(`UPDATE users SET role='admin' WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateAdminSession(ctx, "expired-absolute-test", uid, "console-admin", "", "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE admin_sessions SET absolute_expires_at=now()-interval '1 second' WHERE id=$1`, store.AdminCredentialHash("expired-absolute-test")); err != nil {
		t.Fatal(err)
	}
	if out := request("GET", "/console/api/auth/session", &http.Cookie{Name: adminCookieName, Value: "expired-absolute-test"}, ""); out.Code != 401 {
		t.Fatal("expired absolute lifetime accepted", out.Code)
	}
	if w := request("POST", "/console/api/auth/logout", rotated, initial.CSRF); w.Code != 204 {
		t.Fatalf("logout=%d %s", w.Code, w.Body.String())
	}
	if w := request("GET", "/console/api/auth/session", rotated, ""); w.Code != 401 {
		t.Fatalf("revoked session=%d", w.Code)
	}
	// A client ticket cannot open a Console session; exchange is bound to app/platform.
	ticket := "test-client-ticket"
	meta := model.ClientMeta{AppID: "oronbox", Platform: "android"}
	if _, err := s.CreateLoginTicket(ctx, store.LoginTicketParams{TicketHash: authcore.HashToken(ticket, cfg.SessionSecret), UserID: uid, ExpiresAt: time.Now().Add(time.Minute), Meta: meta, ReturnURI: cfg.ClientRedirectURI}); err != nil {
		t.Fatal(err)
	}
	if w := request("GET", "/console?ticket="+ticket, nil, ""); w.Code != 302 || !strings.Contains(w.Header().Get("Location"), "/console/login") {
		t.Fatalf("ticket bypass=%d", w.Code)
	}
	wrong := store.SessionParams{Meta: model.ClientMeta{AppID: "other", Platform: "ios"}}
	if _, _, err := s.ConsumeLoginTicket(ctx, authcore.HashToken(ticket, cfg.SessionSecret), wrong); err == nil {
		t.Fatal("ticket accepted for another client")
	}
	if _, _, err := s.ConsumeLoginTicket(ctx, authcore.HashToken(ticket, cfg.SessionSecret), store.SessionParams{AccessHash: []byte("at"), RefreshHash: []byte("rt"), AccessExpiresAt: time.Now().Add(time.Hour), RefreshExpiresAt: time.Now().Add(time.Hour), Meta: meta}); err != nil {
		t.Fatal(err)
	}
}

func TestConsoleSessionMigrationHashesExistingCredentials(t *testing.T) {
	db := consoleAuthDatabase(t)
	ctx := context.Background()
	_, err := db.Exec(`ALTER TABLE admin_sessions DROP COLUMN absolute_expires_at,DROP COLUMN credential_hash,DROP COLUMN previous_hash,DROP COLUMN previous_until,DROP COLUMN generation,DROP COLUMN rotated_at;ALTER TABLE login_tickets DROP COLUMN purpose;DELETE FROM schema_migrations WHERE version>=17;INSERT INTO schema_migrations(version)VALUES(16)`)
	if err != nil {
		t.Fatal(err)
	}
	uid := uuid.NewString()
	_, err = db.Exec(`INSERT INTO users(id,bandbbs_user_id,username,role)VALUES($1,456,'migrated-admin','admin');`, uid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO admin_sessions(id,created_at,expires_at,user_id,username)VALUES('old-plaintext-cookie',now()-interval '2 days',now()+interval '6 hours',$1,'migrated-admin')`, uid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO oauth_states(id,provider,purpose,expires_at,app_id,app_version,app_build,platform,return_uri)VALUES('old-console-state','bandbbs','login',now()+interval '1 minute','console','1','1','web','https://console.example/console')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	session, err := store.New(db).AdminSession(ctx, "old-plaintext-cookie")
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "" && session.ID == "old-plaintext-cookie" {
		t.Fatal("plaintext session persisted")
	}
	if session.AbsoluteExpiresAt.After(time.Now().Add(5*24*time.Hour + time.Second)) {
		t.Fatal("migration reset absolute lifetime")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM oauth_states WHERE id='old-console-state'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("old console state survived", err)
	}
}
