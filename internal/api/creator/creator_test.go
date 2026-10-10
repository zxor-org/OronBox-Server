package creator

import (
	"encoding/json"
	"testing"
)

func TestCreatorResourceItemSerialization(t *testing.T) {
	item := CreatorResourceItem{
		ResourceID:      "com.test.app",
		Title:           "测试应用",
		Summary:         "一句话描述",
		Restype:         "quick_app",
		ModerationState: "suspended",
		CurrentVersion:  "1.0.0",
		PendingVersion:  "1.0.1",
		DownloadCount:   100,
		CoinCount:       50,
		Rating:          4.8,
		CreatedAt:       "2026-10-01T00:00:00Z",
		UpdatedAt:       "2026-10-10T00:00:00Z",
	}

	data, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	expectedKeys := []string{"resource_id", "title", "summary", "restype", "moderation_state", "current_version", "pending_version", "download_count", "coin_count", "rating", "created_at", "updated_at"}
	for _, k := range expectedKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key: %s", k)
		}
	}
}
