package admin

import (
	"encoding/json"
	"testing"
)

func TestAdminResItemSerialization(t *testing.T) {
	item := AdminResItem{
		ID:              "com.test.app",
		Name:            "测试资源",
		Kind:            "quick_app",
		Summary:         "摘要",
		Authors:         json.RawMessage(`[]`),
		OwnerUID:        12345,
		Devices:         json.RawMessage(`["xmb9p"]`),
		Attributes:      json.RawMessage(`[]`),
		PaidType:        "free",
		IconSHA256:      "abc",
		CoverSHA256:     "def",
		ModerationState: "visible",
		DownloadCount:   10,
		CoinCount:       5,
		Rating:          4.5,
		RatingCount:     2,
		PublishedAt:     "2026-10-10T00:00:00Z",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	required := []string{"id", "name", "kind", "summary", "authors", "owner_uid", "devices", "attributes", "paid_type", "icon_sha256", "cover_sha256", "moderation_state", "download_count", "coin_count", "rating", "rating_count", "published_at"}
	for _, f := range required {
		if _, ok := m[f]; !ok {
			t.Errorf("missing field: %s", f)
		}
	}
}

func TestPublicationSeedsFanOutAndFailClosed(t *testing.T) {
	raw := json.RawMessage(`{"targets":["bandbbs","astrobox"],"bandbbs":{"categories":{"watch":{"category_id":102,"device_compat_ids":["s4"]}},"purchase":{"link":"https://example.test"}}}`)
	seeds := buildPublicationSeeds([]string{"bandbbs", "astrobox"}, raw, json.RawMessage(`{"xmb9p":{}}`), "")
	if len(seeds) != 2 || seeds[0].CategoryID != 100 || seeds[1].Provider != "astrobox" {
		t.Fatalf("unexpected seeds: %#v", seeds)
	}
	missing := buildPublicationSeeds([]string{"bandbbs"}, json.RawMessage(`{"targets":["bandbbs"]}`), json.RawMessage(`{"unknown":{}}`), "")
	if len(missing) != 1 || missing[0].State != "failed" {
		t.Fatalf("missing mapping should fail closed: %#v", missing)
	}
}
