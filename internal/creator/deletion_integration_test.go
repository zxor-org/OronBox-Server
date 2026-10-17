package creator

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"testing"
)

func TestDeletionHidesFirstRetainsBindingsAndResumesOnlyFailedSteps(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	uid, rid, revision := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,187654,'delete-owner')`, []any{uid}},
		{`INSERT INTO resources(id,owner_id,slug,kind)VALUES($1,$2,'delete','watchface')`, []any{rid, uid}},
		{`INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state)VALUES($1,$2,1,'Delete','short','approved')`, []any{revision, rid}},
		{`UPDATE resources SET current_revision_id=$1 WHERE id=$2`, []any{revision, rid}},
		{`INSERT INTO publications(id,revision_id,target,state)VALUES($1,$2,'bandbbs','pending')`, []any{uuid.NewString(), revision}},
		{`INSERT INTO external_bindings(id,resource_id,provider,external_id)VALUES($1,$2,'bandbbs','{"94":"123","95":"456"}')`, []any{uuid.NewString(), rid}},
		{`INSERT INTO external_bindings(id,resource_id,provider,external_id)VALUES($1,$2,'astrobox','actual-item')`, []any{uuid.NewString(), rid}},
	} {
		if _, err := db.Exec(q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	s := New(db, nil, Limits{})
	calls := map[string]int{}
	fail := true
	s.BandBBSDelete = func(ctx context.Context, owner string, ids []string) error {
		var state string
		db.QueryRow(`SELECT moderation_state FROM resources WHERE id=$1`, rid).Scan(&state)
		if state != "deleted" {
			t.Error("remote deletion happened before local hide")
		}
		calls[ids[0]]++
		if ids[0] == "456" && fail {
			return fmt.Errorf("upstream unavailable")
		}
		return nil
	}
	s.AstroBoxRemove = func(context.Context, string, string, string) (string, error) {
		calls["ab"]++
		return "https://github.com/official/catalog/pull/9", nil
	}
	s.AstroBoxRemovalStatus = func(context.Context, string, string) (string, error) { return "awaiting_external", nil }
	result, err := s.Delete(ctx, uid, rid, []string{"bandbbs", "astrobox"})
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || result.OperationID == "" {
		t.Fatal("delete performed synchronous remote writes")
	}
	for i := 0; i < 3; i++ {
		s.ProcessDeletionStep(ctx)
	}
	op, err := s.DeletionOperation(ctx, uid, result.OperationID)
	if err != nil || len(op.Steps) != 3 {
		t.Fatal(op, err)
	}
	if calls["123"] != 1 || calls["456"] != 1 || calls["ab"] != 1 {
		t.Fatal(calls)
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM external_bindings WHERE resource_id=$1`, rid).Scan(&count)
	if count != 2 {
		t.Fatal("binding lost")
	}
	db.Exec(`UPDATE resource_deletion_steps SET state='failed' WHERE operation_id=$1 AND external_id='456'`, result.OperationID)
	fail = false
	if _, err = s.RetryDeletion(ctx, uuid.NewString(), result.OperationID); err == nil {
		t.Fatal("foreign owner retried deletion")
	}
	if _, err = s.RetryDeletion(ctx, uid, result.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = s.ProcessDeletionStep(ctx); err != nil {
		t.Fatal(err)
	}
	if calls["123"] != 1 || calls["456"] != 2 || calls["ab"] != 1 {
		t.Fatal("successful steps repeated", calls)
	}
	db.Exec(`UPDATE resource_deletion_steps SET next_attempt_at=now() WHERE operation_id=$1 AND provider='astrobox'`, result.OperationID)
	s.AstroBoxRemovalStatus = func(context.Context, string, string) (string, error) { return "completed", nil }
	if err = s.ProcessDeletionStep(ctx); err != nil {
		t.Fatal(err)
	}
	op, err = s.DeletionOperation(ctx, uid, result.OperationID)
	if err != nil || op.State != "completed" || calls["ab"] != 1 {
		t.Fatal("removal PR duplicated", op, err, calls)
	}
	again, err := s.Delete(ctx, uid, rid, []string{"bandbbs", "astrobox"})
	if err != nil || again.OperationID != result.OperationID {
		t.Fatal("delete retry not idempotent", again, err)
	}
}
