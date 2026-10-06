package astrobox

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

func sampleManifest() syndication.Manifest {
	var m syndication.Manifest
	m.Item.ID = "com.example.app"
	m.Item.Restype = "quick_app"
	m.Item.Name = "示例"
	m.Item.Description = "desc"
	m.Item.Icon = "media/icon.webp"
	m.Item.Cover = "media/cover.webp"
	m.Item.Preview = []string{"media/p0.webp"}
	m.Item.Author = []syndication.Author{{Name: "张三", Role: "主程序", UserID: 1, BindABAccount: true}}
	m.Links = []syndication.Link{{Title: "官网", URL: "https://x"}}
	m.Downloads = map[string]syndication.DownloadEntry{
		"xmb9p": {Version: "1.0.0", FileName: "downloads/a.rpk", VersionCode: 3},
	}
	return m
}

func TestBuildManifestV2(t *testing.T) {
	ch := syndication.Changelog{Releases: []syndication.Release{{Version: "1.0.0", Content: "首发"}}}
	raw, err := BuildManifestV2(sampleManifest(), ch, "https://afdian.com/item/x")
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	item := out["item"].(map[string]any)
	authors := item["author"].([]any)
	a0 := authors[0].(map[string]any)
	if _, hasRole := a0["role"]; hasRole {
		t.Fatalf("AB author must not carry role: %v", a0)
	}
	if a0["bindABAccount"] != true {
		t.Fatalf("bindABAccount = %v", a0["bindABAccount"])
	}
	links := out["links"].([]any)
	first := links[0].(map[string]any)
	if first["icon"] != "coins" || first["url"] != "https://afdian.com/item/x" {
		t.Fatalf("purchase link = %v", first)
	}
	dl := out["downloads"].(map[string]any)["xmb9p"].(map[string]any)
	logs := dl["updatelogs"].([]any)
	if len(logs) != 1 || logs[0].(map[string]any)["content"] != "首发" {
		t.Fatalf("updatelogs = %v", logs)
	}
}

func TestBuildResourceCSVFreeIsEmpty(t *testing.T) {
	e := CatalogEntry{ID: "com.x", Name: "X", Restype: "quick_app", RepoOwner: "o", RepoName: "r", RepoCommitHash: "abc1234", Icon: "media/icon.webp", Cover: "media/cover.webp", Tags: "original", DeviceVendors: "xiaomi", Devices: "xmb9p", PaidType: "free"}
	lines := strings.Split(strings.TrimSpace(string(BuildResourceCSV(e))), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header+row, got %d lines", len(lines))
	}
	if !strings.HasSuffix(lines[1], ",") {
		t.Fatalf("free paid_type must serialize empty: %q", lines[1])
	}
}

func TestDeriveReviewStatus(t *testing.T) {
	state, items := DeriveReviewStatus([]string{"[ABCC_NEEDFIX_a] 图标不对"})
	if state != "changes_requested" || len(items) != 1 || items[0].Fixed {
		t.Fatalf("state=%s items=%v", state, items)
	}
	state, items = DeriveReviewStatus([]string{"[ABCC_NEEDFIX_a] 图标不对", "[ABCC_FIXED_a] 已修复"})
	if state != "fixed_waiting" || !items[0].Fixed {
		t.Fatalf("state=%s items=%v", state, items)
	}
	state, _ = DeriveReviewStatus([]string{"普通评论"})
	if state != "waiting_review" {
		t.Fatalf("state=%s", state)
	}
}

func TestDeriveSubmission(t *testing.T) {
	e := CatalogEntry{ID: "com.x"}
	req := DeriveSubmission(e, resourceCSVHead+"\n", "commit1")
	if req.Mode != "create" || req.BaseCatalogCommit == nil {
		t.Fatalf("create req = %#v", req)
	}
	upstream := resourceCSVHead + "\ncom.x,Name,quick_app,o,r,abc,media/icon.webp,media/cover.webp,tags,xiaomi,xmb9p,\n"
	req = DeriveSubmission(e, upstream, "commit2")
	if req.Mode != "edit" || req.OriginalID == nil || req.BaseEntryDigest == nil {
		t.Fatalf("edit req = %#v", req)
	}
}
