package store

import (
	"reflect"
	"testing"
)

func TestInferLegacyAuditDataPreservesMessageCompatibility(t *testing.T) {
	t.Parallel()
	data := inferLegacyAuditData("resource.suspend", "resource=5cf16865-e6ab-4cb7-bd99-36b7b87ee45a previous_moderation=visible moderation=suspended reason=policy")
	if data.Target.Type != "resource" || data.Target.ID != "5cf16865-e6ab-4cb7-bd99-36b7b87ee45a" {
		t.Fatalf("target = %#v", data.Target)
	}
	if !reflect.DeepEqual(data.Before, map[string]any{"moderation_state": "visible"}) || !reflect.DeepEqual(data.After, map[string]any{"moderation_state": "suspended"}) {
		t.Fatalf("before/after = %#v / %#v", data.Before, data.After)
	}
}

func TestInferLegacyAuditTargetsAndReverseLinks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action, message, kind, path string
	}{
		{"user.ban", "user_id=6fa98d9e-91aa-44db-a350-00673ccfa7c2 user=name(1)", "user", "/admin/users/6fa98d9e-91aa-44db-a350-00673ccfa7c2"},
		{"feedback.reply", "ticket=ef99d824-cd37-4977-85ec-f32bb5784d27 ticket_kind=feedback", "feedback", "/admin/feedback/ef99d824-cd37-4977-85ec-f32bb5784d27"},
		{"feedback.resolve", "ticket=c8c8c53a-d5b4-4f8c-b2d4-e192e9acdd58 ticket_kind=resource_report", "ticket", "/admin/reports/c8c8c53a-d5b4-4f8c-b2d4-e192e9acdd58"},
		{"blob.read", "sha256=abc download=1", "blob", "/admin/storage/blobs/abc"},
	}
	for _, test := range tests {
		data := inferLegacyAuditData(test.action, test.message)
		if data.Target.Type != test.kind || data.Target.AdminURL() != test.path {
			t.Errorf("%s: target=%#v url=%q", test.action, data.Target, data.Target.AdminURL())
		}
	}
}

func TestNullableAuditJSON(t *testing.T) {
	t.Parallel()
	if value, err := nullableAuditJSON(nil); err != nil || value != nil {
		t.Fatalf("nil data = %#v, %v", value, err)
	}
	if value, err := nullableAuditJSON(map[string]any{"state": "visible"}); err != nil || string(value.([]byte)) != `{"state":"visible"}` {
		t.Fatalf("structured data = %#v, %v", value, err)
	}
}
