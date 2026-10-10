package community

import (
	"encoding/json"
	"testing"
)

func TestUserMeResponseSerialization(t *testing.T) {
	resp := UserMeResponse{
		BandBBSUID:  123456,
		Username:    "testuser",
		AvatarURL:   "https://example.com/avatar.png",
		Role:        "developer",
		Banned:      false,
		CoinUnits:   120,
		CoinCount:   12,
		CoinBalance: 120,
		GitHubBound: true,
		GitHubLogin: "testgh",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	expectedKeys := []string{"bandbbs_uid", "username", "avatar_url", "role", "banned", "coin_units", "coin_count", "coin_balance", "github_bound", "github_login"}
	for _, k := range expectedKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key: %s", k)
		}
	}
}
