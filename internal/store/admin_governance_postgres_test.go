package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestResourceModerationControlsPublicVisibility(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	ownerID, adminID := uuid.NewString(), uuid.NewString()
	resourceID, revisionID := uuid.NewString(), uuid.NewString()
	blobSeed := strings.ReplaceAll(uuid.NewString(), "-", "")
	blobSHA := blobSeed + blobSeed
	identitySeed := time.Now().UnixNano()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3),($4,$5,$6)`,
		ownerID, identitySeed, "governance-owner-"+ownerID,
		adminID, identitySeed+1, "governance-admin-"+adminID,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM audit_logs WHERE actor_user_id=$1`, adminID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM resources WHERE id=$1`, resourceID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM blobs WHERE sha256=$1`, blobSHA)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id IN ($1,$2)`, ownerID, adminID)
	})
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind) VALUES($1,$2,$3,'watchface')`, resourceID, ownerID, "governance-"+resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state) VALUES($1,$2,1,$3,'governance visibility test','approved')`, revisionID, resourceID, "Governance "+resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO blobs(sha256,size_bytes,media_type,local_key) VALUES($1,1,'image/webp',$2)`, blobSHA, "sha256/"+blobSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO revision_media(id,revision_id,blob_sha256,role,position,width,height) VALUES($1,$2,$3,'preview',0,1,1)`, uuid.NewString(), revisionID, blobSHA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET current_revision_id=$2 WHERE id=$1`, resourceID, revisionID); err != nil {
		t.Fatal(err)
	}

	localBlobs, err := blob.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resourceService := creator.New(db, localBlobs, creator.Limits{})
	resourceStore := store.New(db)
	assertVisible := func(want bool) {
		t.Helper()
		_, err := resourceService.PublicResource(ctx, resourceID)
		if want && err != nil {
			t.Fatalf("public resource unexpectedly hidden: %v", err)
		}
		if !want && !errors.Is(err, creator.ErrNotFound) {
			t.Fatalf("hidden resource remained public: %v", err)
		}
		_, blobErr := resourceStore.PublicBlob(ctx, blobSHA)
		if want && blobErr != nil {
			t.Fatalf("public blob unexpectedly hidden: %v", blobErr)
		}
		if !want && !errors.Is(blobErr, sql.ErrNoRows) {
			t.Fatalf("hidden resource blob remained public: %v", blobErr)
		}
	}

	admin := store.AdminSession{UserID: adminID, Username: "governance-admin-" + adminID}
	assertVisible(true)
	if _, err := resourceStore.AdminManageResource(ctx, resourceID, "suspend", admin); err != nil {
		t.Fatal(err)
	}
	assertVisible(false)
	if _, err := resourceStore.AdminManageResource(ctx, resourceID, "restore", admin); err != nil {
		t.Fatal(err)
	}
	assertVisible(true)
	if _, err := resourceStore.AdminManageResource(ctx, resourceID, "archive", admin); err != nil {
		t.Fatal(err)
	}
	assertVisible(false)
	if _, err := resourceStore.AdminManageResource(ctx, resourceID, "activate", admin); err != nil {
		t.Fatal(err)
	}
	assertVisible(true)

	var actorID string
	if err := db.QueryRowContext(ctx, `SELECT actor_id::text FROM resource_events WHERE resource_id=$1 ORDER BY id DESC LIMIT 1`, resourceID).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	if actorID != adminID {
		t.Fatalf("resource event actor = %q, want %q", actorID, adminID)
	}
}
