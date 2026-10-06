package syndication

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRollupAndFixTags(t *testing.T) {
	if Rollup([]string{"published", "failed"}) != "partially_published" || Rollup([]string{"published"}) != "published" || Rollup([]string{"failed"}) != "syndication_failed" || Rollup(nil) != "pending" || Rollup([]string{"pending"}) != "syndicating" {
		t.Fatal("bad rollup")
	}
	metas := ExtractReviewMeta(`<!-- oronbox-meta: {"role":"reviewer","tag":"NEEDFIX","id":"icon"} --> 和 <!-- oronbox-meta: {"role":"creator","tag":"FIXED","id":"icon"} -->`)
	if len(metas) != 2 || metas[0].Tag != "NEEDFIX" || metas[1].Tag != "FIXED" || metas[0].ID != "icon" {
		t.Fatal(metas)
	}
	if StateDetail(1, errors.New("x"))["can_retry"] != true {
		t.Fatal("retry detail")
	}
	if RetryDelay(0) != 30*time.Second || RetryDelay(10) != 10*time.Minute || ParseCategoryID(" 42 ") != 42 {
		t.Fatal("retry/category helpers")
	}
	ids := CategoryIDs([]struct{ CategoryID int }{{1}, {2}, {1}, {0}})
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatal("category ids", ids)
	}
}

func TestPlanPublications(t *testing.T) {
	downloads := map[string]DownloadEntry{"xmb9p": {FileName: "a.rpk"}, "xmws4xring": {FileName: "a.rpk"}}
	devices := map[string]DeviceSpec{"n67": {ID: "xmb9p", Vendor: "xiaomi"}, "o62m": {ID: "xmws4xring", Vendor: "xiaomi"}}
	publish := PublishConfig{
		ResourcePrefixes: map[string]int{"watchface": 81, "app": 82},
		BandBBS: map[string]BandBBSCategory{
			"n67":  {CategoryID: 100, ForumID: 244, ThreadPrefixes: map[string]int{"watchface": 145, "app": 148}},
			"o62m": {CategoryID: 102, ForumID: 236, ThreadPrefixes: map[string]int{"watchface": 124, "app": 125}},
		},
	}
	plans, err := PlanPublications([]string{"oronbox", "bandbbs", "astrobox"}, downloads, devices, publish)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 4 {
		t.Fatalf("plans=%+v", plans)
	}
	var bandbbs []int
	for _, p := range plans {
		if p.Provider == "bandbbs" {
			bandbbs = append(bandbbs, p.CategoryID)
			if _, ok := p.Config["device_compat_ids"]; !ok {
				t.Fatalf("plan missing device_compat_ids: %+v", p)
			}
			if _, ok := p.Config["resource_prefixes"]; !ok {
				t.Fatalf("plan missing resource_prefixes: %+v", p)
			}
		}
	}
	if len(bandbbs) != 2 || bandbbs[0] != 100 || bandbbs[1] != 102 {
		t.Fatalf("bandbbs categories=%v", bandbbs)
	}
	if _, err := PlanPublications([]string{"bandbbs"}, map[string]DownloadEntry{"unknown": {}}, devices, publish); err == nil {
		t.Fatal("unmapped device accepted")
	}
	if DeviceVendors(downloads, devices) != "xiaomi" {
		t.Fatalf("device vendors = %q", DeviceVendors(downloads, devices))
	}
}

func TestSubmissionCoordinatorSubmit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pulls") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int{"number": 12})
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	c := SubmissionCoordinator{Gitea: GiteaClient{BaseURL: server.URL, Token: "t"}, Owner: "owner", CatalogRepo: "catalog"}
	var manifest Manifest
	manifest.Item.ID = "com.example.app"
	if n, branch, err := c.Submit(context.Background(), manifest.Item.ID, "1.0.0", manifest); err != nil || n != 12 || branch == "" {
		t.Fatalf("submit n=%d branch=%q err=%v", n, branch, err)
	}
	if _, _, err := c.Submit(context.Background(), "", "", manifest); err == nil {
		t.Fatal("invalid submission accepted")
	}
}
