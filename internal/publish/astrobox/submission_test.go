package astrobox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManifestUpdatePreservesForeignConfiguration(t *testing.T) {
	existing := map[string]any{"ext": map[string]any{"trialDownloads": map[string]any{"xmb10": "trial"}, "custom": true}, "item": map[string]any{"name": "old", "custom": "keep"}, "customRoot": 7}
	generated := map[string]any{"ext": map[string]any{}, "item": map[string]any{"name": "new"}}
	result := preserveManifest(existing, generated)
	if result["ext"].(map[string]any)["custom"] != true || result["customRoot"] != 7 || result["item"].(map[string]any)["name"] != "new" || result["item"].(map[string]any)["custom"] != "keep" {
		t.Fatal(result)
	}
}

func TestRetryRecoversCreatedPRAndReusesBranch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/official/catalog/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("head") != "creator:stable" {
			t.Errorf("unexpected head %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`[{"number":42}]`))
	})
	mux.HandleFunc("GET /repos/official/catalog/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":42,"state":"open","html_url":"https://github.com/official/catalog/pull/42","head":{"ref":"stable","repo":{"name":"catalog","owner":{"login":"creator"}}}}`))
	})
	mux.HandleFunc("GET /repos/creator/catalog/git/ref/heads/stable", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"object": map[string]any{"sha": "existing"}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &Client{api: server.URL, http: server.Client()}
	client.cfg.RepoOwner = "official"
	client.cfg.RepoName = "catalog"
	pr, err := client.submissionByHead(context.Background(), "test", "creator:stable")
	if err != nil || pr == nil || pr.Number != 42 {
		t.Fatal(pr, err)
	}
	if err := client.ensureSubmissionBranch(context.Background(), "test", repo{Owner: "creator", Name: "catalog"}, "stable", "base"); err != nil {
		t.Fatal(err)
	}
	if submissionBranch("same-publication") != submissionBranch("same-publication") || submissionBranch("same-publication") == submissionBranch("different-publication") {
		t.Fatal("unstable submission key")
	}
}

func TestPendingSubmissionRecoversOwnResourceButRejectsConflictingIdentity(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/official/catalog/pulls", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"number":42,"head":{"ref":"pending","repo":{"name":"catalog","owner":{"login":"creator"}}}}]`))
	})
	mux.HandleFunc("GET /repos/official/catalog/pulls/42/files", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"filename":"tmp/creator/resource/resource.csv"}]`))
	})
	mux.HandleFunc("GET /repos/creator/catalog/contents/tmp/creator/resource/resource.csv", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"content": base64.StdEncoding.EncodeToString([]byte("id,name\nmy-resource,Name\n"))})
	})
	mux.HandleFunc("GET /repos/official/catalog/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":42,"state":"open","head":{"ref":"pending","repo":{"name":"catalog","owner":{"login":"creator"}}}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &Client{api: server.URL, http: server.Client()}
	client.cfg.RepoOwner = "official"
	client.cfg.RepoName = "catalog"
	pr, err := client.pendingSubmission(context.Background(), "test", "creator", "resource", "my-resource")
	if err != nil || pr == nil || pr.Number != 42 {
		t.Fatal(pr, err)
	}
	if _, err := client.pendingSubmission(context.Background(), "test", "creator", "resource", "other-resource"); err == nil {
		t.Fatal("reused a pending path for a different resource")
	}
}
