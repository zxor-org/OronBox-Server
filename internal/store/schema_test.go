package store

import (
	"context"
	"strings"
	"testing"
)

func TestSchemaDefinesAllDynamicTables(t *testing.T) {
	for _, name := range []string{"users", "oauth_states", "oauth_events", "oauth_grants", "login_tickets", "sessions", "admin_sessions", "github_grants", "resource_interactions", "resource_comments", "resource_coin_votes", "resource_submissions", "resource_collaborators", "publications", "publication_attempts", "external_bindings", "review_cases", "review_case_events", "review_appeals", "moderation_reason_templates", "user_messages", "user_coin_accounts", "daily_coin_checkins", "coin_ledger", "feedback_tickets", "feedback_replies", "feedback_internal_notes", "feedback_status_history", "app_releases", "download_events", "audit_logs", "server_settings", "plugins"} {
		if !strings.Contains(SchemaSQL, "CREATE TABLE IF NOT EXISTS "+name+" ") {
			t.Errorf("missing table %s", name)
		}
	}
	if strings.Contains(SchemaSQL, "schema_version") {
		t.Fatal("schema_version must not be used")
	}
}
func TestOpenRequiresURL(t *testing.T) {
	if _, e := Open(context.Background(), ""); e == nil {
		t.Fatal("empty URL accepted")
	}
}
func TestUninitializedStoreReturnsErrors(t *testing.T) {
	if e := (*Store)(nil).WithTx(context.Background(), nil); e == nil {
		t.Fatal("nil store accepted")
	}
	if e := Migrate(context.Background(), nil); e == nil {
		t.Fatal("nil migration store accepted")
	}
	if e := Schema(context.Background(), nil); e == nil {
		t.Fatal("nil schema store accepted")
	}
	var nilStore *Store
	nilStore.Close()
	if _, e := Open(context.Background(), "not a postgres URL"); e == nil {
		t.Fatal("malformed URL accepted")
	}
}
