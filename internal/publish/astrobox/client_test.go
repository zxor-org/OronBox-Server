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

func TestSyncForkRejectsStaleFork(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/upstream/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"current"}}`))
	})
	mux.HandleFunc("POST /repos/creator/catalog/merge-upstream", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /repos/creator/catalog/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":"stale"}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{api: server.URL, http: server.Client(), cfg: config.AstroBoxConfig{RepoOwner: "upstream", RepoName: "catalog", RepoBranch: "main"}}
	err := client.syncFork(context.Background(), "token", repo{Owner: "creator", Name: "catalog", Branch: "main"})
	if err == nil {
		t.Fatal("stale fork was accepted")
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

func TestSubmissionRequestCreateLeavesEditFieldsNull(t *testing.T) {
	data, err := json.Marshal(submissionRequest{SchemaVersion: 1, Mode: "create"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"schema_version":1,"mode":"create","original_id":null,"base_entry_digest":null,"base_catalog_commit":null}` {
		t.Fatalf("create request = %s", data)
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
