package store_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminAuditStructuredDataLegacyCompatibilityAndExport(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName) })
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	actorID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username,role) VALUES($1,$2,'audit-admin','admin')`, actorID, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	audits := store.New(db)
	actor := store.AdminSession{UserID: actorID, Username: "audit-admin"}
	resourceID := uuid.NewString()
	if err := audits.RecordAuditData(ctx, actor, "resource.test", "success", "127.0.0.1", "audit-test", store.AuditData{
		Message: "structured mutation", Before: map[string]any{"state": "visible"}, After: map[string]any{"state": "suspended"},
		Target: store.AuditTarget{Type: "resource", ID: resourceID, Label: "Test resource"}, Metadata: map[string]any{"request_id": "req-1"},
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-structured-audit writer: the detail reader must still infer its target.
	if _, err := db.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,action,result,user_agent,metadata) VALUES($1,'blob.read','success','legacy-test',$2)`, actorID, []byte(`{"username":"audit-admin","message":"sha256=abc123 download=1"}`)); err != nil {
		t.Fatal(err)
	}

	page, err := audits.AdminAuditLogs(ctx, store.AdminAuditLogQuery{Search: "audit-test", Page: 1, PerPage: 25})
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("structured page = %#v, %v", page, err)
	}
	detail, err := audits.AdminAuditLog(ctx, page.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Target.ID != resourceID || detail.Before["state"] != "visible" || detail.After["state"] != "suspended" || detail.Metadata["request_id"] != "req-1" {
		t.Fatalf("structured detail = %#v", detail)
	}
	legacy, err := audits.AdminAuditLogsForExport(ctx, store.AdminAuditLogQuery{Search: "abc123"})
	if err != nil || len(legacy) != 1 || legacy[0].Target.Type != "blob" || legacy[0].Target.ID != "abc123" {
		t.Fatalf("legacy export = %#v, %v", legacy, err)
	}
}
