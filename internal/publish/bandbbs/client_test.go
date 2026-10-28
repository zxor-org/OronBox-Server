package bandbbs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestTagLineFitsBandBBSCharacterLimit(t *testing.T) {
	value := truncateRunes(strings.Repeat("测", 101), 100)
	if got := len([]rune(value)); got != 100 {
		t.Fatalf("tag line length = %d, want 100", got)
	}
	if got := truncateRunes("short", 100); got != "short" {
		t.Fatalf("short tag line = %q", got)
	}
}

func TestFindMatchingVersionRequiresVersionAndFilename(t *testing.T) {
	versions := []versionSummary{
		{ID: 11, VersionString: "1.0", Files: []struct {
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}{{Filename: "old.rpk", Size: 10}}},
		{ID: 12, VersionString: "1.0", Files: []struct {
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		}{{Filename: "new.rpk", Size: 20}}},
	}
	if got := findMatchingVersion(versions, "1.0", "new.rpk", 20); got == nil || got.ID != 12 {
		t.Fatalf("matched version = %#v, want version 12", got)
	}
	if got := findMatchingVersion(versions, "1.0", "missing.rpk", 20); got != nil {
		t.Fatalf("matched version = %#v, want no match", got)
	}
	if got := findMatchingVersion(versions, "", "new.rpk", 20); got != nil {
		t.Fatalf("matched version = %#v, want no match without version string", got)
	}
}

func TestFindMatchingUpdateRequiresExactContent(t *testing.T) {
	updates := []updateSummary{
		{ID: 21, Title: "更新", Message: "修复问题"},
	}
	if got := findMatchingUpdate(updates, "更新", "修复问题"); got == nil || got.ID != 21 {
		t.Fatalf("matched update = %#v, want update 21", got)
	}
	if got := findMatchingUpdate(updates, "更新", "其他内容"); got != nil {
		t.Fatalf("matched update = %#v, want no match", got)
	}
}

func TestPublishExternalPurchaseUsesResourceMetadata(t *testing.T) {
	t.Parallel()
	var submitted url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/resources/" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		submitted = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"resource":{"resource_id":7568,"view_url":"https://www.bandbbs.cn/resources/7568/"}}`))
	}))
	defer server.Close()

	price := 100.0
	var snap snapshot
	snap.Revision.Name = "测试付费资源"
	snap.Revision.Summary = "外部购买测试"
	snap.Revision.PurchaseLink = "https://ifdian.net/item/example"
	snap.Revision.PurchasePrice = &price
	snap.Revision.PurchaseCurrency = "CNY"
	rawSnapshot, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	rawConfig, err := json.Marshal(config{
		Agreement: true,
		Targets:   []target{{CategoryID: 95}},
	})
	if err != nil {
		t.Fatal(err)
	}

	client := New(server.URL, nil, nil)
	result, err := client.Publish(context.Background(), "token", nil, rawSnapshot, rawConfig)
	if err != nil {
		t.Fatal(err)
	}
	if result.Resources["95"].ResourceID != "7568" {
		t.Fatalf("publish result = %#v", result)
	}
	for key, want := range map[string]string{
		"resource_type":         "external_purchase",
		"external_purchase_url": "https://ifdian.net/item/example",
		"price":                 "100.00",
		"currency":              "CNY",
	} {
		if got := submitted.Get(key); got != want {
			t.Errorf("form %s = %q, want %q", key, got, want)
		}
	}
	if submitted.Has("version_attachment_key") {
		t.Fatal("external purchase unexpectedly submitted a version attachment")
	}
}

func TestBandBBSSelectsExactArtifactWithinSharedPackageID(t *testing.T) {
	var snap snapshot
	if err := json.Unmarshal([]byte(`{"artifacts":[{"blob_sha256":"aaa","package_id":"same.app"},{"blob_sha256":"bbb","package_id":"same.app"}]}`), &snap); err != nil {
		t.Fatal(err)
	}
	index, err := snap.artifactIndex(target{CategoryID: 12, PackageID: "same.app", ArtifactSHA256: "bbb"}, false)
	if err != nil || index != 1 {
		t.Fatalf("selected %d, error %v", index, err)
	}
	if _, err := snap.artifactIndex(target{CategoryID: 12, PackageID: "same.app"}, false); err == nil {
		t.Fatal("ambiguous category package selected silently")
	}
	if _, err := snap.artifactIndex(target{CategoryID: 12, PackageID: "same.app", ArtifactSHA256: "missing"}, false); err == nil {
		t.Fatal("missing bound artifact accepted")
	}
}

func TestBandBBSRelatedLinksAndPartialFailure(t *testing.T) {
	var description string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			return
		}
		if r.Form.Get("resource_category_id") == "13" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"errors":[{"message":"category rejected"}]}`))
			return
		}
		description = r.Form.Get("description")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"resource":{"resource_id":345}}`))
	}))
	defer server.Close()
	client := New(server.URL, nil, nil)
	raw := []byte(`{"revision":{"name":"Test","summary":"Short","description":"Description","purchase_link":"https://example.com/buy","purchase_price":1,"links":[{"title":"Project [site]","url":"https://example.com/project"}]}}`)
	cfg := []byte(`{"agreement":true,"targets":[{"category_id":12},{"category_id":13}]}`)
	result, err := client.Publish(context.Background(), "token", nil, raw, cfg)
	if err == nil {
		t.Fatal("expected second category failure")
	}
	if result.Resources["12"].ResourceID != "345" || result.Resources["12"].URL != "https://www.bandbbs.cn/resources/345/" {
		t.Fatalf("lost completed category and URL: %#v", result)
	}
	if !strings.Contains(description, "[URL=https://example.com/project]Project &#91;site&#93;[/URL]") || !strings.Contains(description, "Description") {
		t.Fatalf("related link not rendered safely: %s", description)
	}
	unsafe := strings.Replace(string(raw), "https://example.com/project", "https://example.com/][IMG]", 1)
	if _, err := client.Publish(context.Background(), "token", nil, []byte(unsafe), cfg); err == nil {
		t.Fatal("unsafe link allowed before external writes")
	}
}
