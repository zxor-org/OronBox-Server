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

func TestReviewCaseHistoryIsAppendedAndImmutable(t *testing.T) {
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

	reviewerID, ownerID := uuid.NewString(), uuid.NewString()
	resourceID, revisionID, caseID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username,role) VALUES($1,$2,'case-reviewer','reviewer'),($3,$4,'case-owner','user')`,
		reviewerID, time.Now().UnixNano(), ownerID, time.Now().UnixNano()+1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind) VALUES($1,$2,'case-resource','watchface')`, resourceID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state) VALUES($1,$2,1,'Case revision','case history fixture','submitted')`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO review_cases(id,revision_id,state) VALUES($1,$2,'pending')`, caseID, revisionID); err != nil {
		t.Fatal(err)
	}

	reviews := store.New(db)
	if err := reviews.AdminAssignReviews(ctx, []string{caseID}, reviewerID, reviewerID); err != nil {
		t.Fatal(err)
	}
	if err := reviews.AdminSaveReviewChecklist(ctx, caseID, reviewerID, []string{"检查图标", "检查安装包"}); err != nil {
		t.Fatal(err)
	}

	events, err := reviews.AdminReviewEvents(ctx, caseID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Event != "assigned" || events[1].Event != "checklist_saved" {
		t.Fatalf("review history = %#v", events)
	}
	if events[0].Actor != "case-reviewer" {
		t.Fatalf("event actor = %q, want the reviewer who acted", events[0].Actor)
	}
	if len(events[1].Checklist) != 2 {
		t.Fatalf("checklist snapshot = %#v, want both items", events[1].Checklist)
	}

	// The history is what an appeal is judged against, so a later edit must be
	// refused by the database rather than trusted to application discipline.
	if _, err := db.ExecContext(ctx, `UPDATE review_case_events SET note='rewritten' WHERE id=$1`, events[0].ID); err == nil {
		t.Fatal("review history accepted an update, it must be immutable")
	}
}
