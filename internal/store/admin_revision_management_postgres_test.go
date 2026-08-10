package store_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminRevisionRollbackReorderAndDiscardPostgres(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	root, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	name := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := root.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.ExecContext(context.Background(), `DROP DATABASE `+name) })
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	ownerID, adminID, collaboratorID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	resourceID, baseID, currentID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	deviceID, artifactID := uuid.NewString(), uuid.NewString()
	sha := strings.Repeat("a", 64)
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,910001,'owner'),($2,910002,'admin'),($3,910003,'collaborator')`, ownerID, adminID, collaboratorID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,draft_name,kind) VALUES($1,$2,'rollback-test','Current','watchface')`, resourceID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,paid_type,state,publication_plan,governance_source,governance_collection_position) VALUES($1,$3,1,'Historical','old','paid','approved','[{"target":"oronbox","config":{}}]','{"author_name":"Old Author"}',7),($2,$3,2,'Current','new','free','approved','[]','{}',0)`, baseID, currentID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET current_revision_id=$2 WHERE id=$1`, resourceID, currentID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO blobs(sha256,size_bytes,media_type,local_key) VALUES($1,1,'image/png','test')`, sha); err != nil {
		t.Fatal(err)
	}
	mediaIDs := []string{uuid.NewString(), uuid.NewString(), uuid.NewString()}
	for i, id := range mediaIDs {
		if _, err := db.ExecContext(ctx, `INSERT INTO revision_media(id,revision_id,blob_sha256,role,position,width,height) VALUES($1,$2,$3,'preview',$4,1,1)`, id, baseID, sha, i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO devices(id,codename,display_name,platform,enabled) VALUES($1,'rollback-device','Rollback Device','vela_os',true)`, deviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO revision_artifacts(id,revision_id,blob_sha256,original_name,package_format,package_id,package_version,analysis) VALUES($1,$2,$3,'resource.bin','zip','pkg','1.0','{}')`, artifactID, baseID, sha); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO revision_artifact_devices(revision_id,artifact_id,device_id) VALUES($1,$2,$3)`, baseID, artifactID, deviceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revision_collaborators(revision_id,user_id) VALUES($1,$2)`, baseID, collaboratorID); err != nil {
		t.Fatal(err)
	}

	s := store.New(db)
	actor := store.AdminSession{UserID: adminID, Username: "admin"}
	draftID, err := s.AdminCreateRollbackRevision(ctx, resourceID, baseID, actor)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.AdminResourceRevision(ctx, resourceID, draftID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Revision.Name != "Historical" || detail.Revision.BaseRevisionID != baseID || detail.Revision.State != "draft" || detail.Revision.CreatedVia != "admin" {
		t.Fatalf("unexpected rollback revision: %#v", detail.Revision)
	}
	governance, err := s.AdminRevisionGovernance(ctx, draftID)
	if err != nil {
		t.Fatal(err)
	}
	if governance.AuthorName != "Old Author" || len(governance.CollaboratorIDs) != 1 || governance.CollaboratorIDs[0] != collaboratorID {
		t.Fatalf("governance was not cloned: %#v", governance)
	}
	if len(detail.Media) != 3 || len(detail.Artifacts) != 1 {
		t.Fatalf("assets were not cloned: media=%d artifacts=%d", len(detail.Media), len(detail.Artifacts))
	}

	moveID := detail.Media[2].ID
	if err := s.AdminMoveRevisionMedia(ctx, resourceID, draftID, moveID, 0); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT id::text,position FROM revision_media WHERE revision_id=$1 AND role='preview' ORDER BY position`, draftID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	positions := 0
	for rows.Next() {
		var id string
		var position int
		if err := rows.Scan(&id, &position); err != nil {
			t.Fatal(err)
		}
		if position != positions {
			t.Fatalf("position=%d want %d", position, positions)
		}
		if positions == 0 && id != moveID {
			t.Fatalf("moved media=%s want %s", id, moveID)
		}
		positions++
	}
	if err := s.AdminMoveRevisionMedia(ctx, resourceID, baseID, mediaIDs[0], 0); !errors.Is(err, store.ErrAdminResourceConflict) {
		t.Fatalf("historical revision was mutable: %v", err)
	}

	if err := s.AdminDiscardRevisionDraft(ctx, resourceID, draftID, actor); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM resource_revisions WHERE id=$1`, draftID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("draft remained: count=%d err=%v", count, err)
	}
	if err := s.AdminDiscardRevisionDraft(ctx, resourceID, baseID, actor); !errors.Is(err, store.ErrAdminResourceConflict) {
		t.Fatalf("historical revision was discarded: %v", err)
	}

	draftID, err = s.AdminCreateRollbackRevision(ctx, resourceID, baseID, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AdminSubmitRevisionDraft(ctx, resourceID, draftID, actor); err != nil {
		t.Fatal(err)
	}
	secondDetail, err := s.AdminResourceRevision(ctx, resourceID, draftID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM review_cases WHERE revision_id=$1 AND state='pending'`, draftID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rollback did not enter review: count=%d err=%v", count, err)
	}
	// A pending review is the only submitted state in which the review
	// workbench may correct metadata and assets in place.
	_, err = s.AdminSaveRevisionDraft(ctx, resourceID, store.AdminRevisionDraftInput{
		DraftRevisionID: draftID, BaseRevisionID: baseID, Name: "Reviewer corrected",
		Summary: "checked in review", PaidType: "free", PublicationPlan: []byte(`[]`),
	}, actor)
	if err != nil {
		t.Fatalf("correct pending review: %v", err)
	}
	if err := s.AdminMoveRevisionMedia(ctx, resourceID, draftID, secondDetail.Media[0].ID, 1); err != nil {
		t.Fatalf("edit pending review media: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE review_cases SET state='approved' WHERE revision_id=$1`, draftID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminSaveRevisionDraft(ctx, resourceID, store.AdminRevisionDraftInput{
		DraftRevisionID: draftID, BaseRevisionID: baseID, Name: "Too late",
		PaidType: "free", PublicationPlan: []byte(`[]`),
	}, actor); !errors.Is(err, store.ErrAdminResourceConflict) {
		t.Fatalf("decided review remained mutable: %v", err)
	}
}
