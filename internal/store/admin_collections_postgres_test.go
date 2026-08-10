package store_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminCollectionRevisionAppliesLifecycleAndOrderedSnapshotOnApproval(t *testing.T) {
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

	ownerID, adminID := uuid.NewString(), uuid.NewString()
	collectionID, currentRevisionID := uuid.NewString(), uuid.NewString()
	firstID, secondID := uuid.NewString(), uuid.NewString()
	identity := time.Now().UnixNano()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,bandbbs_user_id,username,role) VALUES($1,$2,$3,'user'),($4,$5,$6,'admin')`, []any{ownerID, identity, "owner", adminID, identity + 1, "admin"}},
		{`INSERT INTO resources(id,owner_id,slug,draft_name,kind) VALUES($1,$2,'first','First','watchface'),($3,$2,'second','Second','watchface')`, []any{firstID, ownerID, secondID}},
		{`INSERT INTO resource_collections(id,owner_id,slug,kind,representative_resource_id) VALUES($1,$2,'suite','watchface',$3)`, []any{collectionID, ownerID, firstID}},
		{`INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary,state,enabled,representative_resource_id,created_by) VALUES($1,$2,1,'Suite','Current','approved',true,$3,$4)`, []any{currentRevisionID, collectionID, firstID, ownerID}},
		{`INSERT INTO resource_collection_revision_members(id,revision_id,resource_id,resource_slug,resource_name,position) VALUES(md5(random()::text||clock_timestamp()::text)::uuid,$1,$2,'first','First',0)`, []any{currentRevisionID, firstID}},
		{`UPDATE resource_collections SET current_revision_id=$2 WHERE id=$1`, []any{collectionID, currentRevisionID}},
		{`UPDATE resources SET collection_id=$2,collection_position=0 WHERE id=$1`, []any{firstID, collectionID}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}

	adminStore := store.New(db)
	draft, err := adminStore.AdminUpdateCollectionMetadata(ctx, collectionID, store.AdminCollectionMetadataInput{Name: "Renamed", Summary: "Pending", Enabled: false, RepresentativeResourceID: secondID, ResourceIDs: []string{secondID, firstID}, CreatedBy: adminID})
	if err != nil {
		t.Fatal(err)
	}
	var enabled bool
	var representative, current string
	if err := db.QueryRowContext(ctx, `SELECT enabled,representative_resource_id::text,current_revision_id::text FROM resource_collections WHERE id=$1`, collectionID).Scan(&enabled, &representative, &current); err != nil {
		t.Fatal(err)
	}
	if !enabled || representative != firstID || current != currentRevisionID {
		t.Fatalf("draft changed live collection: enabled=%v representative=%s current=%s", enabled, representative, current)
	}

	service := creator.New(db, nil, creator.Limits{})
	if err := service.ReviewCollection(ctx, draft.ID, adminID, true, "approved"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT enabled,representative_resource_id::text,current_revision_id::text FROM resource_collections WHERE id=$1`, collectionID).Scan(&enabled, &representative, &current); err != nil {
		t.Fatal(err)
	}
	if enabled || representative != secondID || current != draft.ID {
		t.Fatalf("approved snapshot not applied: enabled=%v representative=%s current=%s", enabled, representative, current)
	}
	rows, err := db.QueryContext(ctx, `SELECT id::text,collection_position FROM resources WHERE collection_id=$1 ORDER BY collection_position`, collectionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		var position int
		if err := rows.Scan(&id, &position); err != nil {
			t.Fatal(err)
		}
		if position != len(ids) {
			t.Fatalf("position=%d want %d", position, len(ids))
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 || ids[0] != secondID || ids[1] != firstID {
		t.Fatalf("applied order=%v", ids)
	}

	rejected, err := adminStore.AdminUpdateCollectionMetadata(ctx, collectionID, store.AdminCollectionMetadataInput{Name: "Rejected", Enabled: true, ResourceIDs: []string{firstID}, CreatedBy: adminID})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ReviewCollection(ctx, rejected.ID, adminID, false, "no"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT enabled,current_revision_id::text FROM resource_collections WHERE id=$1`, collectionID).Scan(&enabled, &current); err != nil {
		t.Fatal(err)
	}
	if enabled || current != draft.ID {
		t.Fatalf("rejected revision changed live state: enabled=%v current=%s", enabled, current)
	}
	detail, err := adminStore.AdminCollection(ctx, collectionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Revisions) != 3 || len(detail.Revisions[1].Members) != 2 || detail.Revisions[0].State != "rejected" {
		t.Fatalf("history is incomplete: %#v", detail.Revisions)
	}
}
