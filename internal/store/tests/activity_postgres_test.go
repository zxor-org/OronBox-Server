package store_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestActivityDeduplicationAndMigration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { adminDB.Close() })
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName); err != nil {
			t.Errorf("cleanup test database: %v", err)
		}
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

	s := store.New(db)
	u := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,1,'activity')`, u); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.RecordActivity(ctx, u, "1.0", "android", now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err := s.RecordActivity(ctx, u, "1.1", "android", now); err != nil {
		t.Fatal(err)
	}
	stats, err := s.ActivityStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DAU != 1 || stats.WAU != 1 || stats.MAU != 1 || len(stats.Coverage) != 2 || len(stats.Daily) != 30 {
		t.Fatalf("stats=%+v", stats)
	}
	users, total, err := s.ActivityUsers(ctx, "1.1", "android", 1)
	if err != nil || total != 1 || len(users) != 1 || users[0].ID != u {
		t.Fatalf("users=%+v total=%d err=%v", users, total, err)
	}

	other := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,2,'older')`, other); err != nil {
		t.Fatal(err)
	}
	for _, days := range []int{-7, -30, 1} {
		if _, err := s.RecordActivity(ctx, other, "older", "ios", now.AddDate(0, 0, days)); err != nil {
			t.Fatal(err)
		}
	}
	stats, err = s.ActivityStats(ctx)
	if err != nil || stats.DAU != 1 || stats.WAU != 1 || stats.MAU != 2 {
		t.Fatalf("window stats=%+v err=%v", stats, err)
	}
	receipt := store.ActivityDay(time.Date(2026, 10, 17, 16, 0, 0, 0, time.UTC))
	if receipt.Date != "2026-10-18" || receipt.NextDayStart != time.Date(2026, 10, 18, 16, 0, 0, 0, time.UTC) {
		t.Fatalf("receipt=%+v", receipt)
	}
	// Verify the incremental upgrade independently of a fresh install.
	if _, err := db.ExecContext(ctx, `ALTER TABLE admin_sessions DROP COLUMN absolute_expires_at,DROP COLUMN credential_hash,DROP COLUMN previous_hash,DROP COLUMN previous_until,DROP COLUMN generation,DROP COLUMN rotated_at;ALTER TABLE login_tickets DROP COLUMN purpose;DROP TABLE user_client_usage_daily;DROP TABLE user_activity_daily;DELETE FROM schema_migrations WHERE version>=16;INSERT INTO schema_migrations(version)VALUES(15) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordActivity(ctx, u, "2", "ios", now); err != nil {
		t.Fatal(err)
	}
}
