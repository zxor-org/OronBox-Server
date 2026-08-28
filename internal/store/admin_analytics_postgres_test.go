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

func TestAdminAnalyticsSeriesAndTotals(t *testing.T) {
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

	now := time.Now().UTC()
	oldUserID := uuid.NewString()
	newUserID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username,role,created_at) VALUES($1,$2,'old-user','user',$3),($4,$5,'new-user','user',$6)`, oldUserID, now.Add(-20*24*time.Hour).UnixNano(), now.Add(-20*24*time.Hour), newUserID, now.UnixNano(), now); err != nil {
		t.Fatal(err)
	}

	resourceID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,draft_name,kind,moderation_state) VALUES($1,$2,'analytics-resource','资源','quickapp','visible')`, resourceID, oldUserID); err != nil {
		t.Fatal(err)
	}
	revisionID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state,created_by) VALUES($1,$2,1,'资源','','approved',$3)`, revisionID, resourceID, oldUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET current_revision_id=$1 WHERE id=$2`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}
	blobSHA := strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO blobs(sha256,size_bytes,media_type,local_key) VALUES($1,1024,'application/zip','analytics-artifact')`, blobSHA); err != nil {
		t.Fatal(err)
	}
	artifactID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO revision_artifacts(id,revision_id,blob_sha256,original_name,package_format) VALUES($1,$2,$3,'package.zip','zip')`, artifactID, revisionID, blobSHA); err != nil {
		t.Fatal(err)
	}
	downloads := []time.Time{
		now.Add(-2 * 24 * time.Hour),
		now.Add(-2 * 24 * time.Hour),
		now.Add(-2 * 24 * time.Hour),
		now.Add(-1 * 24 * time.Hour),
		now.Add(-40 * 24 * time.Hour),
	}
	for _, at := range downloads {
		if _, err := db.ExecContext(ctx, `INSERT INTO download_events(resource_id,artifact_id,user_id,created_at) VALUES($1,$2,$3,$4)`, resourceID, artifactID, newUserID, at); err != nil {
			t.Fatal(err)
		}
	}

	adminStore := store.New(db)
	result, err := adminStore.AdminAnalytics(ctx, "30d")
	if err != nil {
		t.Fatal(err)
	}
	if result.Totals.TotalUsers != 2 {
		t.Fatalf("total users = %d, want 2", result.Totals.TotalUsers)
	}
	if result.Totals.TotalDownloads != 5 {
		t.Fatalf("total downloads = %d, want 5", result.Totals.TotalDownloads)
	}
	// Three downloads two days ago plus one yesterday = 4 inside the 7-day
	// window; the 40-day-old row falls outside the 30-day window as well.
	if result.Totals.Downloads7d != 4 {
		t.Fatalf("downloads 7d = %d, want 4", result.Totals.Downloads7d)
	}
	if result.Totals.Downloads30d != 4 {
		t.Fatalf("downloads 30d = %d, want 4", result.Totals.Downloads30d)
	}
	if result.Totals.NewUsers30d != 2 {
		t.Fatalf("new users 30d = %d, want 2", result.Totals.NewUsers30d)
	}
	if len(result.UserGrowth) != 30 {
		t.Fatalf("user growth buckets = %d, want 30", len(result.UserGrowth))
	}
	if len(result.Downloads) != 30 {
		t.Fatalf("download buckets = %d, want 30", len(result.Downloads))
	}
}
