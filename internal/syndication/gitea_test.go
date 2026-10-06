package syndication

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGiteaClientRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token t" {
			t.Fatal("missing auth")
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()
	c := GiteaClient{BaseURL: srv.URL, Token: "t"}
	if e := c.CreateRepository(context.Background(), "o", "r", true); e != nil {
		t.Fatal(e)
	}
	if e := c.CreateFile(context.Background(), "o", "r", "manifest.json", "main", "init", []byte("{}")); e != nil {
		t.Fatal(e)
	}
}

func TestGiteaClientErrorsAndUpsert(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sha":"old"}`))
			return
		}
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	c := GiteaClient{BaseURL: server.URL, Token: "t"}
	if err := c.UpsertFile(context.Background(), "o", "r", "manifest.json", "main", "update", []byte("{}")); err != nil {
		t.Fatal(err)
	}
}

func TestGiteaClientPullRequestAndRepositorySettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 7})
		case http.MethodPatch:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := GiteaClient{BaseURL: server.URL, Token: "t"}
	if n, err := c.CreatePullRequest(context.Background(), "o", "r", "title", "head", "main", "body"); err != nil || n != 7 {
		t.Fatalf("pr n=%d err=%v", n, err)
	}
	if err := c.SetPrivate(context.Background(), "o", "r", false); err != nil {
		t.Fatal(err)
	}
}

func TestGiteaCommitFilesTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/branches/main"):
			_, _ = w.Write([]byte(`{"commit":{"id":"parentsha"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			_, _ = w.Write([]byte(`{"sha":"blobsha"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			_, _ = w.Write([]byte(`{"sha":"treesha"}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			_, _ = w.Write([]byte(`{"sha":"commitsha"}`))
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := GiteaClient{BaseURL: server.URL, Token: "t"}
	sha, err := c.CommitFilesTree(context.Background(), "o", "r", "main", "msg", map[string][]byte{"manifest.json": []byte("{}"), "media/icon.webp": []byte("x")})
	if err != nil || sha != "commitsha" {
		t.Fatalf("sha=%s err=%v", sha, err)
	}
}
