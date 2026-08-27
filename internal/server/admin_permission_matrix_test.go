package server

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

type adminAuthConnector struct{ role string }

func (connector adminAuthConnector) Connect(context.Context) (driver.Conn, error) {
	return adminAuthConn{role: connector.role}, nil
}

func (adminAuthConnector) Driver() driver.Driver { return adminAuthDriver{} }

type adminAuthDriver struct{}

func (adminAuthDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("admin auth test driver requires a connector")
}

type adminAuthConn struct{ role string }

func (adminAuthConn) Prepare(string) (driver.Stmt, error)      { return nil, driver.ErrSkip }
func (adminAuthConn) Close() error                             { return nil }
func (adminAuthConn) Begin() (driver.Tx, error)                { return nil, driver.ErrSkip }
func (adminAuthConn) CheckNamedValue(*driver.NamedValue) error { return nil }

func (connection adminAuthConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	now := time.Now().UTC()
	switch {
	case strings.Contains(query, "FROM admin_sessions"):
		return &adminAuthRows{
			columns: []string{"id", "user_id", "username", "expires_at"},
			values:  [][]driver.Value{{"test-session", "test-user", connection.role, now.Add(time.Hour)}},
		}, nil
	case strings.Contains(query, "FROM users WHERE id"):
		return &adminAuthRows{
			columns: []string{"id", "bandbbs_user_id", "username", "avatar_url", "role", "banned_at", "ban_reason", "creator_frozen_at", "created_at", "updated_at"},
			values:  [][]driver.Value{{"test-user", int64(1), connection.role, "", connection.role, nil, "", nil, now, now}},
		}, nil
	default:
		return nil, errors.New("deliberate backend conflict")
	}
}

func (adminAuthConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}

type adminAuthRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (rows *adminAuthRows) Columns() []string { return rows.columns }
func (rows *adminAuthRows) Close() error      { return nil }
func (rows *adminAuthRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(destination, rows.values[rows.index])
	rows.index++
	return nil
}

func adminPermissionTestApp(t *testing.T, role string) *App {
	t.Helper()
	db := sql.OpenDB(adminAuthConnector{role: role})
	t.Cleanup(func() { _ = db.Close() })
	return &App{
		cfg:       config.Config{PublicURL: "https://admin.example", SessionSecret: "test-session-secret"},
		store:     store.New(db),
		creator:   creator.New(db, nil, creator.Limits{}),
		templates: web.NewTemplates(),
	}
}

func performAdminRequest(t *testing.T, app *App, method, target, body, origin string) *httptest.ResponseRecorder {
	t.Helper()
	return performAdminRequestWithToken(t, app, method, target, body, origin, app.adminCSRFToken("test-session"))
}

func performAdminRequestWithToken(t *testing.T, app *App, method, target, body, origin, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: "test-session"})
	if body != "" {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if token != "" {
		request.Header.Set(adminCSRFHeader, token)
	}
	recorder := httptest.NewRecorder()
	app.Routes().ServeHTTP(recorder, request)
	return recorder
}

// sensitiveAdminMutationPaths spans one route per admin domain that changes
// state, so the CSRF and origin guards are proven on all of them rather than on
// whichever one happens to be convenient.
var sensitiveAdminMutationPaths = []string{
	"/admin/review/revision-1",
	"/admin/resources/resource-1/draft",
	"/admin/resources/resource-1/draft/revision-1/submit",
	"/admin/publications/publication-1",
	"/admin/users/user-1/state",
	"/admin/coins/users",
	"/admin/devices/device-1",
	"/admin/resource-attributes",
	"/admin/cleanup/preview",
	"/admin/cleanup/execute",
	"/admin/plugins/plugin-1/package",
}

func TestAdminRoutePermissionMatrix(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		path     string
		body     string
		reviewer bool
	}{
		{name: "resource review", method: http.MethodPost, path: "/admin/review/revision-1", reviewer: true},
		{name: "review checklist save", method: http.MethodPost, path: "/admin/review/review-1/checklist", body: "item=preview", reviewer: true},
		{name: "review bulk", method: http.MethodPost, path: "/admin/review/bulk", body: "bulk_action=priority&priority=1&review_ids=review-1", reviewer: true},
		{name: "comment review", method: http.MethodPost, path: "/admin/comments/comment-1", body: "action=approve", reviewer: true},
		{name: "comment bulk", method: http.MethodPost, path: "/admin/comments/bulk", body: "action=approve&comment_ids=comment-1", reviewer: true},
		{name: "collection review", method: http.MethodPost, path: "/admin/collections/revision-1", reviewer: true},
		{name: "resource management revision", method: http.MethodPost, path: "/admin/resources/resource-1/draft/revision-1/governance", reviewer: true},
		{name: "collection management revision", method: http.MethodPost, path: "/admin/collections/collection-1/draft", reviewer: true},
		{name: "plugin management revision", method: http.MethodPost, path: "/admin/plugins/plugin-1/metadata", reviewer: true},
		{name: "publication execution", method: http.MethodPost, path: "/admin/publications/publication-1", body: "action=requeue"},
		{name: "publication filtered batch retry", method: http.MethodPost, path: "/admin/publications/retry-failed", body: "state=failed&target=astrobox"},
		{name: "resource online state", method: http.MethodPost, path: "/admin/resources/resource-1/state", body: "state=listed"},
		{name: "user state", method: http.MethodPost, path: "/admin/users/user-1/state", body: "state=banned"},
		{name: "coin adjustment", method: http.MethodPost, path: "/admin/coins/users", body: "action=adjust&user_id=user-1&delta_units=1&reason=test"},
		{name: "device update", method: http.MethodPost, path: "/admin/devices/device-1", body: "display_name=Device"},
		{name: "settings mutation", method: http.MethodPost, path: "/admin/resource-attributes", body: "name=test"},
		{name: "moderation prompt", method: http.MethodPost, path: "/admin/comments/prompt", body: "prompt=test"},
		{name: "release publishing", method: http.MethodPost, path: "/admin/releases"},
		{name: "blob retry", method: http.MethodPost, path: "/admin/storage/blobs/sha/requeue"},
		{name: "maintenance preview", method: http.MethodPost, path: "/admin/cleanup/preview"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, role := range []string{"reviewer", "admin"} {
				t.Run(role, func(t *testing.T) {
					recorder := performAdminRequest(t, adminPermissionTestApp(t, role), test.method, test.path, test.body, "https://admin.example")
					if role == "reviewer" && !test.reviewer {
						if recorder.Code != http.StatusForbidden {
							t.Fatalf("reviewer status = %d, want 403; body=%q", recorder.Code, recorder.Body.String())
						}
						return
					}
					if recorder.Code == http.StatusForbidden || recorder.Code == http.StatusTemporaryRedirect || recorder.Code == http.StatusFound && recorder.Header().Get("Location") == "/admin/login" {
						t.Fatalf("%s was rejected by authorization: status=%d body=%q", role, recorder.Code, recorder.Body.String())
					}
				})
			}
		})
	}
}

// A reviewer who reaches an admin-only page should get an explanation inside
// the console, not a bare "forbidden" that reads like the console is broken.
// A blocked write still gets a plain status, because nobody reads the body of
// a rejected form post as a document.
func TestReviewerGetsAnInConsoleExplanationInsteadOfBareForbidden(t *testing.T) {
	page := performAdminRequest(t, adminPermissionTestApp(t, "reviewer"), http.MethodGet, "/admin/users", "", "")
	if page.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", page.Code)
	}
	body := page.Body.String()
	for _, expected := range []string{"<html", "admin-drawer", "这个板块只对管理员开放", "/admin/users"} {
		if !strings.Contains(body, expected) {
			t.Errorf("the forbidden page is missing %q", expected)
		}
	}
	// The explanation is rendered in the shell, so it must carry the reviewer's
	// drawer rather than leaking the admin-only destinations it just refused.
	if strings.Contains(body, `href="/admin/settings"`) {
		t.Error("the forbidden page still shows admin-only navigation")
	}

	write := performAdminRequest(t, adminPermissionTestApp(t, "reviewer"), http.MethodPost, "/admin/users/user-1/state", "state=banned", "https://admin.example")
	if write.Code != http.StatusForbidden {
		t.Fatalf("blocked write status = %d, want 403", write.Code)
	}
	if strings.Contains(write.Body.String(), "<html") {
		t.Error("a blocked form post should not render a full page")
	}
}

func TestRevokedAdminRoleInvalidatesExistingSession(t *testing.T) {
	recorder := performAdminRequest(t, adminPermissionTestApp(t, "user"), http.MethodGet, "/admin/review", "", "")
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want 403; body=%q", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != adminCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("revoked role did not clear admin session cookie: %#v", cookies)
	}
}

func TestReviewerCannotChangePublicationConfiguration(t *testing.T) {
	body := "draft_revision_id=revision-1&publication_plan=%7B%22astrobox%22%3Atrue%7D"
	reviewer := performAdminRequest(t, adminPermissionTestApp(t, "reviewer"), http.MethodPost, "/admin/resources/resource-1/draft", body, "https://admin.example")
	if reviewer.Code != http.StatusForbidden || !strings.Contains(reviewer.Body.String(), "publication configuration") {
		t.Fatalf("reviewer status=%d body=%q", reviewer.Code, reviewer.Body.String())
	}
	admin := performAdminRequest(t, adminPermissionTestApp(t, "admin"), http.MethodPost, "/admin/resources/resource-1/draft", body, "https://admin.example")
	if admin.Code != http.StatusConflict {
		t.Fatalf("admin did not reach draft persistence: status=%d body=%q", admin.Code, admin.Body.String())
	}
}

func TestEverySensitiveAdminMutationRejectsCrossOriginRequests(t *testing.T) {
	for _, path := range sensitiveAdminMutationPaths {
		t.Run(path, func(t *testing.T) {
			// The token is supplied, so only the origin mismatch can reject.
			recorder := performAdminRequest(t, adminPermissionTestApp(t, "admin"), http.MethodPost, path, "action=test", "https://evil.example")
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// The token is the layer that still holds when a browser or intermediary drops
// the Origin and Referer headers, so no mutating route may accept a request
// without it.
func TestEverySensitiveAdminMutationRequiresCSRFToken(t *testing.T) {
	for _, path := range sensitiveAdminMutationPaths {
		t.Run(path, func(t *testing.T) {
			for name, token := range map[string]string{"absent": "", "forged": "not-the-session-token"} {
				t.Run(name, func(t *testing.T) {
					recorder := performAdminRequestWithToken(t, adminPermissionTestApp(t, "admin"), http.MethodPost, path, "action=test", "https://admin.example", token)
					if recorder.Code != http.StatusForbidden {
						t.Fatalf("status=%d body=%q", recorder.Code, recorder.Body.String())
					}
				})
			}
		})
	}
}

// Server-rendered forms cannot set a header, so the hidden field has to be
// accepted on exactly the same terms.
func TestAdminMutationAcceptsCSRFTokenFromHiddenFormField(t *testing.T) {
	app := adminPermissionTestApp(t, "admin")
	body := url.Values{
		"action":          {"requeue"},
		web.CSRFFieldName: {app.adminCSRFToken("test-session")},
	}.Encode()
	recorder := performAdminRequestWithToken(t, app, http.MethodPost, "/admin/publications/publication-1", body, "https://admin.example", "")
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("hidden CSRF field was not accepted: body=%q", recorder.Body.String())
	}
}

func TestAdminStateMutationsExposeConflictWithoutLeakingBackendDetails(t *testing.T) {
	tests := []struct{ path, body string }{
		{path: "/admin/publications/publication-1", body: "action=requeue"},
		{path: "/admin/devices/device-1", body: "display_name=Device&codename=device&platform=vela_os"},
		{path: "/admin/resources/resource-1/draft/revision-1/submit"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			recorder := performAdminRequest(t, adminPermissionTestApp(t, "admin"), http.MethodPost, test.path, test.body, "https://admin.example")
			if recorder.Code != http.StatusConflict {
				t.Fatalf("status=%d, want 409; body=%q", recorder.Code, recorder.Body.String())
			}
			if strings.Contains(strings.ToLower(recorder.Body.String()), "secret") {
				t.Fatalf("response leaked credential-shaped data: %q", recorder.Body.String())
			}
		})
	}
}

func TestHTMXAdminEndpointsReuseSameAuth(t *testing.T) {
	paths := []string{
		"/admin/review?q=music",
		"/admin/comments?state=review",
		"/admin/review/review-1/checklist",
	}
	for _, path := range paths {
		t.Run("anonymous "+path, func(t *testing.T) {
			method := http.MethodGet
			body := ""
			if strings.Contains(path, "checklist") {
				method = http.MethodPost
				body = "item=preview"
			}
			request := httptest.NewRequest(method, path, strings.NewReader(body))
			if body != "" {
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			request.Header.Set("HX-Request", "true")
			recorder := httptest.NewRecorder()
			adminPermissionTestApp(t, "admin").Routes().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusFound && recorder.Code != http.StatusSeeOther && recorder.Code != http.StatusTemporaryRedirect {
				t.Fatalf("anonymous HX status=%d, want redirect to login; location=%q body=%q", recorder.Code, recorder.Header().Get("Location"), recorder.Body.String())
			}
			if loc := recorder.Header().Get("Location"); !strings.Contains(loc, "/admin/login") {
				t.Fatalf("anonymous HX location=%q, want login", loc)
			}
		})
	}

	t.Run("reviewer can post checklist with HX", func(t *testing.T) {
		app := adminPermissionTestApp(t, "reviewer")
		request := httptest.NewRequest(http.MethodPost, "/admin/review/review-1/checklist", strings.NewReader("item=preview"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Origin", "https://admin.example")
		request.Header.Set(adminCSRFHeader, app.adminCSRFToken("test-session"))
		request.AddCookie(&http.Cookie{Name: adminCookieName, Value: "test-session"})
		recorder := httptest.NewRecorder()
		app.Routes().ServeHTTP(recorder, request)
		if recorder.Code == http.StatusForbidden || recorder.Code == http.StatusUnauthorized {
			t.Fatalf("reviewer HX checklist rejected: status=%d body=%q", recorder.Code, recorder.Body.String())
		}
	})
}

func TestPluginPackageMultipartUploadRequiresCSRFToken(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("package", "plugin.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("PK\x03\x04")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/admin/plugins/plugin-1/package", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Origin", "https://admin.example")
	request.AddCookie(&http.Cookie{Name: adminCookieName, Value: "test-session"})
	recorder := httptest.NewRecorder()
	adminPermissionTestApp(t, "admin").Routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("multipart plugin upload without CSRF token: status=%d body=%q", recorder.Code, recorder.Body.String())
	}
}
