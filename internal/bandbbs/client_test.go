package bandbbs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublishLocalWithThreadPrefix(t *testing.T) {
	var sawPrefix string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/resource-categories/"):
			_, _ = w.Write([]byte(`{"categories":[{"resource_category_id":100,"title":"米环9Pro","can_add":true,"allow_local":true,"allow_external":true,"enable_versioning":true,"min_tags":0}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attachments/new-key"):
			_, _ = w.Write([]byte(`{"key":"K1","attachment":{"direct_url":"https://cdn/att/1"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resources/"):
			_, _ = w.Write([]byte(`{"resource":{"resource_id":4201,"view_url":"https://www.bandbbs.cn/resources/4201/"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resource-versions/"):
			_, _ = w.Write([]byte(`{"version":{"resource_version_id":13509,"files":[{"id":74254}]}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resource-updates/"):
			_, _ = w.Write([]byte(`{"update":{"resource_update_id":12923}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/resources/4201/"):
			_, _ = w.Write([]byte(`<a href="/threads/999/">讨论区</a>`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/threads/999/"):
			_ = r.ParseForm()
			sawPrefix = r.Form.Get("prefix_id")
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := Client{APIURL: srv.URL, HTTP: srv.Client()}
	res, err := c.Publish(context.Background(), "token", PublishInput{
		CategoryID: 100, Restype: "quick_app", ResourcePrefixID: 82, ThreadPrefixID: 148,
		Title: "Shell++", TagLine: "终端工具箱", Description: "正文",
		Previews: []PreviewImage{{Name: "p1.webp", Data: []byte("x")}},
		Version:  "1.0.2", VersionFileName: "app.rpk", VersionData: []byte("rp"),
		VersionTitle: "1.0.2", VersionMessage: "修复",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ResourceID != 4201 || res.VersionID != 13509 || res.ThreadID != 999 || res.UpdateID != 12923 {
		t.Fatalf("unexpected result: %#v", res)
	}
	if sawPrefix != "148" {
		t.Fatalf("thread prefix = %q, want 148", sawPrefix)
	}
	if len(res.FileIDs) != 1 || res.FileIDs[0] != 74254 {
		t.Fatalf("file ids = %v", res.FileIDs)
	}
}

func TestPublishExternalPurchaseSkipsVersion(t *testing.T) {
	versionCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/resource-categories/"):
			_, _ = w.Write([]byte(`{"categories":[{"resource_category_id":100,"can_add":true,"allow_external":true,"allow_local":false,"enable_versioning":true}]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resources/"):
			_ = r.ParseForm()
			if r.Form.Get("resource_type") != "external_purchase" || r.Form.Get("price") != "5.00" {
				t.Errorf("form = %v", r.Form)
			}
			_, _ = w.Write([]byte(`{"resource":{"resource_id":4202}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resource-versions/"):
			versionCalled = true
			_, _ = w.Write([]byte(`{"version":{"resource_version_id":1}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/resources/4202/"):
			_, _ = w.Write([]byte(`threads/1/`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	c := Client{APIURL: srv.URL, HTTP: srv.Client()}
	res, err := c.Publish(context.Background(), "token", PublishInput{
		CategoryID: 100, Restype: "watchface", ResourcePrefixID: 82,
		Title: "归序", TagLine: "相册表盘", Description: "正文",
		External: &ExternalPurchase{Link: "https://afdian.com/item/x", Price: 5, Currency: "CNY"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ResourceID != 4202 || versionCalled {
		t.Fatalf("res=%#v versionCalled=%v", res, versionCalled)
	}
}

func TestMissingScopeDetection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"errors":[{"code":"missing_scope","message":"need thread:write"}]}`))
	}))
	defer srv.Close()
	c := Client{APIURL: srv.URL, HTTP: srv.Client()}
	err := c.SetThreadPrefix(context.Background(), "t", 1, 2)
	if !MissingScope(err) {
		t.Fatalf("expected missing_scope, got %v", err)
	}
}
