package creator

import (
	"context"
	"github.com/google/uuid"
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
