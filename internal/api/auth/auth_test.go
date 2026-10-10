package auth

import (
	"encoding/json"
	"testing"
)

func TestTicketExchangeResponseSerialization(t *testing.T) {
	resp := TicketExchangeResponse{
		AccessToken:  "test_access_token",
		RefreshToken: "test_refresh_token",
		UserUID:      12345,
		ExpiresIn:    2592000,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if m["access_token"] != "test_access_token" {
		t.Errorf("unexpected access_token: %v", m["access_token"])
	}
	if m["user_uid"] != float64(12345) {
		t.Errorf("unexpected user_uid: %v", m["user_uid"])
	}
}
