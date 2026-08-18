package astrobox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

func TestSyncForkAlignsStaleFork(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/upstream/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"current"}}`))
	})
	mux.HandleFunc("POST /repos/creator/catalog/merge-upstream", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	forkReads := 0
	mux.HandleFunc("GET /repos/creator/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		forkReads++
		sha := "stale"
		if forkReads > 1 {
			sha = "current"
		}
		_, _ = w.Write([]byte(`{"object":{"sha":"` + sha + `"}}`))
	})
	mux.HandleFunc("PATCH /repos/creator/catalog/git/refs/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{api: server.URL, http: server.Client(), cfg: config.AstroBoxConfig{RepoOwner: "upstream", RepoName: "catalog", RepoBranch: "main"}}
	err := client.syncFork(context.Background(), "token", repo{Owner: "creator", Name: "catalog", Branch: "main"})
	if err != nil {
		t.Fatalf("stale fork was not aligned: %v", err)
	}
	if forkReads != 2 {
		t.Fatalf("fork branch was read %d times, want 2", forkReads)
	}
}

func TestSyncForkCreatesMissingRefAfterUpdate404(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/upstream/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"current"}}`))
	})
	mux.HandleFunc("POST /repos/creator/catalog/merge-upstream", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	forkReads := 0
	mux.HandleFunc("GET /repos/creator/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		forkReads++
		if forkReads == 2 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
			return
		}
		sha := "stale"
		if forkReads > 2 {
			sha = "current"
		}
		_, _ = w.Write([]byte(`{"object":{"sha":"` + sha + `"}}`))
	})
	mux.HandleFunc("PATCH /repos/creator/catalog/git/refs/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	created := false
	mux.HandleFunc("POST /repos/creator/catalog/git/refs", func(w http.ResponseWriter, r *http.Request) {
		created = true
		w.WriteHeader(http.StatusCreated)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{api: server.URL, http: server.Client(), cfg: config.AstroBoxConfig{RepoOwner: "upstream", RepoName: "catalog", RepoBranch: "main"}}
	if err := client.syncFork(context.Background(), "token", repo{Owner: "creator", Name: "catalog", Branch: "main"}); err != nil {
		t.Fatalf("missing fork ref was not recovered: %v", err)
	}
	if !created {
		t.Fatal("missing fork ref was not created")
	}
}

func TestPrepareCatalogSnapshotAllowsStaleForkWhenSyncIsNotWritable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/upstream/catalog/forks", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"name":"catalog","default_branch":"main","owner":{"login":"creator"}}`))
	})
	mux.HandleFunc("GET /repos/creator/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"stale"}}`))
	})
	mux.HandleFunc("POST /repos/creator/catalog/merge-upstream", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	mux.HandleFunc("GET /repos/upstream/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"current"}}`))
	})
	mux.HandleFunc("PATCH /repos/creator/catalog/git/refs/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	mux.HandleFunc("POST /repos/creator/catalog/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	})
	catalog := []byte("id,name,restype,repo_owner,repo_name,repo_commit_hash,icon,cover,tags,device_vendors,devices,paid_type\n")
	mux.HandleFunc("GET /repos/upstream/catalog/contents/index_v2.csv", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString(catalog) + `","sha":"file-sha"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{api: server.URL, http: server.Client(), cfg: config.AstroBoxConfig{
		RepoOwner: "upstream", RepoName: "catalog", RepoBranch: "main", CatalogPath: "index_v2.csv",
	}}
	snapshot, err := client.prepareCatalogSnapshot(context.Background(), "token")
	if err != nil {
		t.Fatalf("stale fork should not block a v2 snapshot: %v", err)
	}
	if snapshot.Commit != "current" || snapshot.ForkBase != "stale" {
		t.Fatalf("snapshot = %#v, want upstream current and stale fork base", snapshot)
	}
}

func TestNormalizePaidTypeForAstroBox(t *testing.T) {
	tests := map[string]string{"free": "", " FREE ": "", "paid": "paid", "force_paid": "force_paid"}
	for input, want := range tests {
		if got := normalizePaid(input); got != want {
			t.Errorf("normalizePaid(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestPurchaseLinksUseSupportedIcon(t *testing.T) {
	links := purchaseLinks("  https://example.com/full  ")
	if len(links) != 1 {
		t.Fatalf("purchaseLinks() returned %d links, want 1", len(links))
	}
	if got := links[0]["icon"]; got != "coins" {
		t.Fatalf("purchase link icon = %q, want coins", got)
	}
	if got := links[0]["title"]; got != purchaseLinkTitle {
		t.Fatalf("purchase link title = %q, want %q", got, purchaseLinkTitle)
	}
	if got := links[0]["url"]; got != "https://example.com/full" {
		t.Fatalf("purchase link URL = %q, want trimmed URL", got)
	}
	if got := purchaseLinks(" "); len(got) != 0 {
		t.Fatalf("empty purchase link produced %#v", got)
	}
}

func TestBuildManifestCarriesExternalPurchaseLink(t *testing.T) {
	price := 100.0
	var snap snapshot
	snap.Revision.Name = "测试资源"
	snap.Revision.Summary = "外部购买资源"
	snap.Revision.PurchaseLink = "https://ifdian.net/item/example"
	snap.Revision.PurchasePrice = &price
	snap.Revision.PurchaseCurrency = "CNY"

	manifest := buildManifest(
		snap,
		publishConfig{ItemID: "resource-1"},
		"quick_app",
		"media/icon.png",
		"media/cover.png",
		[]map[string]any{{"name": "creator", "bindABAccount": false}},
		[]string{"media/preview.png"},
		map[string]map[string]string{
			"xmb10": {"version": "1.0.0", "file_name": "downloads/app.rpk"},
		},
	)

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	links, ok := decoded["links"].([]any)
	if !ok || len(links) != 1 {
		t.Fatalf("manifest links = %#v, want one link", decoded["links"])
	}
	link, ok := links[0].(map[string]any)
	if !ok {
		t.Fatalf("manifest link = %#v, want object", links[0])
	}
	for key, want := range map[string]string{
		"title": "购买链接",
		"url":   "https://ifdian.net/item/example",
		"icon":  "coins",
	} {
		if got := link[key]; got != want {
			t.Errorf("manifest link %s = %#v, want %q", key, got, want)
		}
	}
}

func TestAstroBoxV2DeviceFilter(t *testing.T) {
	if !IsSupportedDeviceID("xmb10") {
		t.Fatal("xmb10 should be supported by AstroBox V2")
	}
	for _, id := range []string{"m67", "l61", "xmrw4", "n65", "n66nfc"} {
		if IsSupportedDeviceID(id) {
			t.Fatalf("%s is not an AstroBox V2 device ID", id)
		}
	}
	artifacts := []artifact{{Devices: []deviceRef{{ID: "m67"}, {ID: "xmb10"}, {ID: "xmrw4"}}}}
	if got := uniqueDevices(artifacts); len(got) != 1 || got[0] != "xmb10" {
		t.Fatalf("uniqueDevices() = %v, want [xmb10]", got)
	}
}

func TestAstroBoxCatalogUsesLowercaseSupportedVendors(t *testing.T) {
	if got := uniqueVendors([]artifact{{Platform: "vela_os", Devices: []deviceRef{{ID: "xmb10"}}}, {Platform: "zepp_os", Devices: []deviceRef{{ID: "other"}}}}); len(got) != 1 || got[0] != "xiaomi" {
		t.Fatalf("uniqueVendors() = %v, want [xiaomi]", got)
	}
	row := []string{"item", "name", "quick_app", "owner", "repo", "abcdef1", "icon.png", "cover.png", "tools", "xiaomi", "xmb10", ""}
	if err := validateCatalogRow(row); err != nil {
		t.Fatalf("valid catalog row rejected: %v", err)
	}
	row[9] = "Xiaomi"
	if err := validateCatalogRow(row); err == nil {
		t.Fatal("uppercase vendor accepted")
	}
	row[9] = "unsupported-vendor"
	if err := validateCatalogRow(row); err == nil {
		t.Fatal("unsupported vendor accepted")
	}
}

func TestSubmissionPathNormalizesGitHubNames(t *testing.T) {
	client := &Client{}
	got, err := client.submissionPath("OrPudding", "NeoMusic-AstroBox-Resource")
	if err != nil {
		t.Fatal(err)
	}
	if got != "tmp/orpudding/neomusic-astrobox-resource" {
		t.Fatalf("submission path = %q", got)
	}
	if _, err := client.submissionPath("bad/name", "repo"); err == nil {
		t.Fatal("path traversal segment was accepted")
	}
}

func TestCatalogRowDigestIsStable(t *testing.T) {
	row := []string{"item", "name", "quick_app", "owner", "repo", "abcdef1", "icon.png", "cover.png", "tools", "xiaomi", "xmb10", ""}
	one, err := catalogRowDigest(row)
	if err != nil {
		t.Fatal(err)
	}
	two, err := catalogRowDigest(append([]string(nil), row...))
	if err != nil {
		t.Fatal(err)
	}
	if one != two || len(one) != 64 {
		t.Fatalf("digest is not stable: %q != %q", one, two)
	}
}

func TestSubmissionRequestCreateKeepsCatalogCommitAndLeavesEditFieldsNull(t *testing.T) {
	commit := "ffb9e96a7d423b0bf261a6850fc09c83bdc08c46"
	data, err := json.Marshal(submissionRequest{
		SchemaVersion:     1,
		Mode:              "create",
		BaseCatalogCommit: &commit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"schema_version":1,"mode":"create","original_id":null,"base_entry_digest":null,"base_catalog_commit":"ffb9e96a7d423b0bf261a6850fc09c83bdc08c46"}` {
		t.Fatalf("create request = %s", data)
	}
}

func TestGithubRequestErrorIncludesRequestDetails(t *testing.T) {
	err := (&githubRequestError{
		status:   http.StatusNotFound,
		method:   http.MethodPost,
		endpoint: "https://api.github.com/repos/creator/catalog/git/refs?ref=main",
		body:     `{"message":"Not Found"}`,
	}).Error()
	for _, part := range []string{"POST", "/repos/creator/catalog/git/refs?ref=main", "HTTP 404", `"message":"Not Found"`} {
		if !strings.Contains(err, part) {
			t.Fatalf("error %q does not contain %q", err, part)
		}
	}
}

func TestCheckCatalogEntry(t *testing.T) {
	row := []string{"item", "name", "quick_app", "owner", "repo", "abcdef1", "icon.png", "cover.png", "tools", "xiaomi", "xmb10", ""}
	csvData := "id,name,restype,repo_owner,repo_name,repo_commit_hash,icon,cover,tags,device_vendors,devices,paid_type\n" + strings.Join(row, ",") + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/upstream/catalog/contents/index_v2.csv", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":"` + base64.StdEncoding.EncodeToString([]byte(csvData)) + `","sha":"file-sha"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &Client{api: server.URL, http: server.Client(), cfg: config.AstroBoxConfig{RepoOwner: "upstream", RepoName: "catalog", RepoBranch: "main", CatalogPath: "index_v2.csv"}}
	check, err := client.CheckCatalogEntry(context.Background(), "token", "item", row)
	if err != nil {
		t.Fatal(err)
	}
	if !check.Found || !check.Matches {
		t.Fatalf("catalog check = %#v", check)
	}
	row[1] = "changed"
	check, err = client.CheckCatalogEntry(context.Background(), "token", "item", row)
	if err != nil {
		t.Fatal(err)
	}
	if !check.Found || check.Matches {
		t.Fatalf("mismatched catalog check = %#v", check)
	}
}
