package astrobox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDownloadPathsShareContentAndDisambiguateNames(t *testing.T) {
	a := []artifact{
		{Blob: "one", Name: "app.rpk", Devices: []deviceRef{{ID: "xmb10"}}},
		{Blob: "one", Name: "renamed.rpk", Devices: []deviceRef{{ID: "xmb9"}}},
		{Blob: "two", Name: "app.rpk", Devices: []deviceRef{{ID: "xmb9p"}}},
	}
	paths, err := artifactPaths(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths["one"] != "downloads/app.rpk" || paths["two"] != "downloads/app-xmb9p.rpk" {
		t.Fatalf("unexpected layout: %#v", paths)
	}
	a = append(a, artifact{Blob: "three", Name: "app-xmb9p.rpk", Devices: []deviceRef{{ID: "xmb9"}}})
	paths, err = artifactPaths(a)
	if err != nil || paths["three"] == paths["two"] {
		t.Fatalf("filename collision overwrites another package: %#v %v", paths, err)
	}
}

func TestMainResourceBranchUsesMainWithoutOverwritingDefaultBranch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/creator/resource/git/ref/heads/main", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("GET /repos/creator/resource/git/ref/heads/master", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"object":{"sha":"source"}}`)) })
	created := false
	mux.HandleFunc("POST /repos/creator/resource/git/refs", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["ref"] != "refs/heads/main" || body["sha"] != "source" {
			t.Errorf("incorrect main branch source: %#v", body)
		}
		created = true
		w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	c := &Client{api: server.URL, http: server.Client()}
	result, err := c.mainResourceBranch(context.Background(), "token", repo{Owner: "creator", Name: "resource", Branch: "master"})
	if err != nil || !created || result.Branch != "main" {
		t.Fatalf("main branch setup failed: %#v %v", result, err)
	}
}

func TestResourcePRTitlePreservesSubmissionAction(t *testing.T) {
	if got := resourcePRTitle("create", "New name", ""); got != "[OBCC] Add new resource: New name" {
		t.Fatal(got)
	}
	if got := resourcePRTitle("edit", "New name", "[ABCC] Add new resource: Old name"); got != "[ABCC] Add new resource: New name" {
		t.Fatal(got)
	}
	if got := resourcePRTitle("edit", "New name", "[OBCC] Add Old name"); got != "[OBCC] Add new resource: New name" {
		t.Fatal(got)
	}
	if got := resourcePRTitle("edit", "New name", ""); got != "[OBCC] Update resource: New name" {
		t.Fatal(got)
	}
}
