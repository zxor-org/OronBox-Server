package syndication

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadPublicationConfigUsesBundledBaseline(t *testing.T) {
	devices, publish, err := LoadPublicationConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) == 0 || len(publish.BandBBS) == 0 {
		t.Fatal("bundled publication mappings are empty")
	}
	if publish.ResourcePrefixes["quick_app"] == 0 {
		t.Fatal("quick_app prefix was not normalized")
	}
}

func TestPublicationConfigSourceLoadsFromGitea(t *testing.T) {
	devices := []byte(`{"n67":{"id":"xmb9p"}}`)
	publish := []byte(`{"resource_prefixes":{"app":82},"bandbbs":{"n67":{"category_id":95,"thread_prefixes":{"app":122}}}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ref") != "release/config" {
			t.Fatalf("ref=%q", r.URL.Query().Get("ref"))
		}
		var content []byte
		if r.URL.Path == "/api/v1/repos/o/r/contents/devices.json" {
			content = devices
		} else if r.URL.Path == "/api/v1/repos/o/r/contents/publish.json" {
			content = publish
		} else {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"encoding": "base64",
			"content":  base64.StdEncoding.EncodeToString(content),
		})
	}))
	defer server.Close()

	source := PublicationConfigSource{
		Client: GiteaClient{BaseURL: server.URL},
		Owner:  "o",
		Repo:   "r",
		Ref:    "release/config",
	}
	gotDevices, gotPublish, err := source.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotDevices["n67"].ID != "xmb9p" || gotPublish.BandBBS["n67"].CategoryID != 95 {
		t.Fatalf("unexpected mappings: %#v %#v", gotDevices, gotPublish)
	}
}

func TestPublicationConfigSourceDoesNotFallbackWhenGiteaConfigured(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	source := PublicationConfigSource{
		Client:   GiteaClient{BaseURL: server.URL},
		Owner:    "o",
		Repo:     "r",
		LocalDir: "missing-local-dir",
	}
	if _, _, err := source.Load(context.Background()); err == nil {
		t.Fatal("expected configured Gitea failure")
	}
}
