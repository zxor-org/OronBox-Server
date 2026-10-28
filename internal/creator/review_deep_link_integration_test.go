package creator

import (
	"context"
	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"testing"
	"time"
)

func TestReviewNotificationOpensRequestedCaseNotLatest(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	owner, res, oldRev, newRev, oldCase, newCase := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,$2,'owner')`, owner, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind)VALUES($1,$2,'deep-link','quickapp')`, res, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state)VALUES($1,$2,1,'old','old','rejected'),($3,$2,2,'new','new','approved')`, oldRev, res, newRev); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO review_cases(id,revision_id,state,note)VALUES($1,$2,'rejected','old decision'),($3,$4,'approved','new decision')`, oldCase, oldRev, newCase, newRev); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET current_revision_id=$1 WHERE id=$2`, newRev, res); err != nil {
		t.Fatal(err)
	}
	service := New(db, nil, Limits{})
	w, err := service.ReviewWorkspace(ctx, owner, oldCase)
	if err != nil {
		t.Fatal(err)
	}
	if w.Review == nil || w.Review.ID != oldCase || w.Review.Note != "old decision" {
		t.Fatalf("wrong case: %#v", w.Review)
	}
	if w.CurrentRevision == nil || w.CurrentRevision.ID != newRev || len(w.Revisions) != 2 {
		t.Fatal("review link discarded normal editing workspace")
	}
	if _, err := service.ReviewWorkspace(ctx, uuid.NewString(), oldCase); err != ErrNotFound {
		t.Fatalf("foreign owner: %v", err)
	}
}

func TestReviewPageKeepsLocalAndExternalStatesSeparate(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	owner, res, revision, review, pub := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,$2,'review-owner')`, []any{owner, time.Now().UnixNano()}},
		{`INSERT INTO resources(id,owner_id,slug,kind)VALUES($1,$2,'external-review','quickapp')`, []any{res, owner}},
		{`INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state)VALUES($1,$2,1,'review','summary','approved')`, []any{revision, res}},
		{`INSERT INTO review_cases(id,revision_id,state)VALUES($1,$2,'approved')`, []any{review, revision}},
		{`INSERT INTO publications(id,revision_id,target,state,config)VALUES($1,$2,'astrobox','reviewing','{}')`, []any{pub, revision}},
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	service := New(db, nil, Limits{})
	if err := service.RecordAstroBoxReview(ctx, owner, review, astrobox.Timeline{State: "changes_requested"}); err != nil {
		t.Fatal(err)
	}
	page, err := service.ReviewPage(ctx, owner, "", 30)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	called := false
	if err := service.RequestAstroBoxReopen(ctx, uuid.NewString(), review, func() error { called = true; return nil }); err != ErrNotFound || called {
		t.Fatalf("foreign reopen: called=%t err=%v", called, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET moderation_state='frozen' WHERE id=$1`, res); err != nil {
		t.Fatal(err)
	}
	if err := service.RequestAstroBoxReopen(ctx, owner, review, func() error { called = true; return nil }); err != ErrConflict || called {
		t.Fatalf("frozen reopen: called=%t err=%v", called, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET moderation_state='visible' WHERE id=$1`, res); err != nil {
		t.Fatal(err)
	}
	if err := service.RequestAstroBoxReopen(ctx, owner, review, func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("valid reopen: called=%t err=%v", called, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE publications SET status_detail=status_detail||'{"pull_request_number":42}'::jsonb WHERE id=$1`, pub); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state) VALUES($1,$2,2,'saved draft','summary','draft')`, uuid.NewString(), res); err != nil {
		t.Fatal(err)
	}
	workspace, err := service.Workspace(ctx, owner, res)
	if err != nil || workspace.ActiveAstroBoxSubmission == nil || workspace.ActiveAstroBoxSubmission.ReviewID != review || workspace.ActiveAstroBoxSubmission.PullRequestNumber != 42 || workspace.Review != nil {
		t.Fatalf("draft lost active submission: %#v %v", workspace.ActiveAstroBoxSubmission, err)
	}
	item := page.Items[0]
	if item.State != ReviewApproved || item.PublicationState != "reviewing" || item.ExternalReviewState != "changes_requested" {
		t.Fatalf("bad state contract: %#v", item)
	}
}

func TestReviewNotificationUpgradeFromV18(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version=19; INSERT INTO schema_migrations(version) VALUES(18);
 CREATE OR REPLACE FUNCTION notify_review_result() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM rrev.version FROM resource_revisions rrev WHERE rrev.id=NEW.revision_id;RETURN NEW;END $$;`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	owner, res, revision, review := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,$2,'notification-owner')`, []any{owner, time.Now().UnixNano()}},
		{`INSERT INTO resources(id,owner_id,slug,kind)VALUES($1,$2,'notification-upgrade','quickapp')`, []any{res, owner}},
		{`INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state)VALUES($1,$2,1,'Updated title','summary','approved')`, []any{revision, res}},
		{`INSERT INTO review_cases(id,revision_id,state)VALUES($1,$2,'pending')`, []any{review, revision}},
		{`UPDATE review_cases SET state='approved' WHERE id=$1`, []any{review}},
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	var title string
	if err := db.QueryRowContext(ctx, `SELECT data->>'resource_name' FROM user_messages WHERE user_id=$1 AND event='review.approved'`, owner).Scan(&title); err != nil || title != "Updated title" {
		t.Fatalf("notification title=%q err=%v", title, err)
	}
}
