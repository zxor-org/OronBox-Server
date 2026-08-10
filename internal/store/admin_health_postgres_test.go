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

func TestAdminHealthDiagnosticsAndCleanupPreviewTransaction(t *testing.T) {
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

	userID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username,role) VALUES($1,$2,'health-admin','admin')`, userID, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Now().UTC()
	oldExpiry, newExpiry := cutoff.Add(-time.Hour), cutoff.Add(time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO oauth_states(id,provider,purpose,expires_at,app_id,app_version,app_build,platform,return_uri) VALUES ('old-state','bandbbs','login',$1,'app','1','1','web','https://example.com'),('new-state','bandbbs','login',$2,'app','1','1','web','https://example.com')`, oldExpiry, newExpiry); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO login_tickets(id,ticket_hash,user_id,expires_at,app_id,platform,return_uri) VALUES ($1,$2,$3,$4,'app','web','https://example.com'),($5,$6,$3,$7,'app','web','https://example.com')`, uuid.NewString(), []byte("old-ticket"), userID, oldExpiry, uuid.NewString(), []byte("new-ticket"), newExpiry); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO admin_sessions(id,expires_at,user_id,username) VALUES ('old-admin',$1,$3,'health-admin'),('new-admin',$2,$3,'health-admin')`, oldExpiry, newExpiry, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO user_messages(id,user_id,kind,title,body,expires_at) VALUES ($1,$2,'admin_message','old','old',$3),($4,$2,'admin_message','new','new',$5)`, uuid.NewString(), userID, oldExpiry, uuid.NewString(), newExpiry); err != nil {
		t.Fatal(err)
	}

	adminStore := store.New(db)
	diagnostics, err := adminStore.AdminHealthDiagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics.DatabaseSizeBytes <= 0 || diagnostics.DatabaseSessions <= 0 {
		t.Fatalf("database diagnostics are incomplete: %#v", diagnostics)
	}
	preview, err := adminStore.PreviewExpiredCleanup(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Total() != 4 || preview.OAuthStates != 1 || preview.LoginTickets != 1 || preview.AdminSessions != 1 || preview.UserMessages != 1 {
		t.Fatalf("unexpected cleanup preview: %#v", preview)
	}
	deleted, err := adminStore.ExecuteExpiredCleanup(ctx, preview)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.OAuthStates != 1 || deleted.LoginTickets != 1 || deleted.AdminSessions != 1 || deleted.UserMessages != 1 {
		t.Fatalf("unexpected cleanup result: %#v", deleted)
	}
	for table := range map[string]struct{}{"oauth_states": {}, "login_tickets": {}, "admin_sessions": {}, "user_messages": {}} {
		var remaining int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != 1 {
			t.Fatalf("%s remaining rows = %d, want 1 newer than preview cutoff", table, remaining)
		}
	}
}
