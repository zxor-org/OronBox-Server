package store_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminUserGovernance(t *testing.T) {
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
	t.Cleanup(func() {
		_, _ = adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName)
	})
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

	userID, adminID := uuid.NewString(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3),($4,$5,$6)`,
		userID, time.Now().UnixNano(), "governance-target",
		adminID, time.Now().UnixNano()+1, "governance-actor",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO sessions(id,user_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,app_id,app_version,platform) VALUES($1,$2,$3,$4,now()+interval '1 hour',now()+interval '1 day','app','1.0','linux')`,
		uuid.NewString(), userID, []byte("access-"+userID), []byte("refresh-"+userID)); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	admin := store.AdminSession{UserID: adminID, Username: "governance-actor"}

	if _, err := s.AdminManageUser(ctx, adminID, "ban", "", "", admin); !errors.Is(err, store.ErrAdminResourceConflict) {
		t.Fatalf("self ban error = %v, want conflict", err)
	}
	item, err := s.AdminManageUser(ctx, userID, "ban", "spam", "", admin)
	if err != nil {
		t.Fatal(err)
	}
	if item.BannedAt == nil || item.BanReason != "spam" {
		t.Fatalf("banned item = %#v", item)
	}
	var activeSessions int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&activeSessions); err != nil {
		t.Fatal(err)
	}
	if activeSessions != 0 {
		t.Fatalf("ban left %d active sessions", activeSessions)
	}
	if _, err := s.UserByAccessToken(ctx, []byte("access-"+userID)); !errors.Is(err, store.ErrInvalidCredential) {
		t.Fatalf("banned session still resolves: %v", err)
	}
	item, err = s.AdminManageUser(ctx, userID, "freeze_creator", "", "", admin)
	if err != nil {
		t.Fatal(err)
	}
	if item.CreatorFrozenAt == nil {
		t.Fatal("creator_frozen_at was not set")
	}
	if _, err := s.AdminManageUser(ctx, userID, "set_role", "", "superadmin", admin); !errors.Is(err, store.ErrAdminResourceConflict) {
		t.Fatalf("invalid role error = %v, want conflict", err)
	}
	item, err = s.AdminManageUser(ctx, userID, "set_role", "", "reviewer", admin)
	if err != nil {
		t.Fatal(err)
	}
	if item.Role != "reviewer" {
		t.Fatalf("role = %q", item.Role)
	}
	item, err = s.AdminManageUser(ctx, userID, "unban", "", "", admin)
	if err != nil {
		t.Fatal(err)
	}
	if item.BannedAt != nil || item.BanReason != "" {
		t.Fatalf("unbanned item = %#v", item)
	}
	item, err = s.AdminManageUser(ctx, userID, "unfreeze_creator", "", "", admin)
	if err != nil {
		t.Fatal(err)
	}
	if item.CreatorFrozenAt != nil {
		t.Fatal("creator_frozen_at survived unfreeze")
	}
	var accountMessages int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND kind='account'`, userID).Scan(&accountMessages); err != nil {
		t.Fatal(err)
	}
	if accountMessages != 5 {
		t.Fatalf("account messages = %d, want 5", accountMessages)
	}
	sent, err := s.CreateAdminMessages(ctx, []string{userID, adminID, userID, "invalid"}, "Maintenance", "Tonight")
	if err != nil {
		t.Fatal(err)
	}
	if sent != 2 {
		t.Fatalf("admin message recipients = %d, want 2", sent)
	}
	if err := s.CreateAnnouncement(ctx, adminID, "Release", "Available now"); err != nil {
		t.Fatal(err)
	}
	items, unread, err := s.UserMessages(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 7 || len(items) != 7 {
		t.Fatalf("messages = %d unread = %d, want 7", len(items), unread)
	}
	if items[0].Type == "" || items[0].Type != items[0].Kind {
		t.Fatalf("message type alias = %#v", items[0])
	}
	if _, err := db.ExecContext(ctx, `UPDATE user_messages SET expires_at=now()-interval '1 second' WHERE user_id=$1 AND kind='admin_message'`, userID); err != nil {
		t.Fatal(err)
	}
	_, unread, err = s.UserMessages(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if unread != 6 {
		t.Fatalf("unread after expiry = %d, want 6", unread)
	}
}
