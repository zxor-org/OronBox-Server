package creator

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

func TestCollectionSubmissionIsAtomicVersionedAndExplicitlyReplacesPending(t *testing.T) {
	db := testDatabase(t)
	ctx := context.Background()
	owner := uuid.NewString()
	first, second := uuid.NewString(), uuid.NewString()
	db.Exec(`INSERT INTO users(id,bandbbs_user_id,username)VALUES($1,991771,'collection-owner')`, owner)
	for i, id := range []string{first, second} {
		rev := uuid.NewString()
		if _, err := db.Exec(`INSERT INTO resources(id,owner_id,slug,kind)VALUES($1,$2,$3,'watchface')`, id, owner, "member-"+id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state)VALUES($1,$2,1,$3,'short','approved')`, rev, id, "Member "+string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
		db.Exec(`UPDATE resources SET current_revision_id=$1 WHERE id=$2`, rev, id)
	}
	s := New(db, nil, Limits{})
	in := CollectionSubmission{Slug: "full-collection", Name: "Old live title", Summary: "short", Description: "# body", Kind: Watchface, ResourceIDs: []string{first, second}, RepresentativeID: first}
	c, err := s.SubmitCollection(ctx, owner, "", in)
	if err != nil {
		t.Fatal(err)
	}
	if c.CurrentRevisionID != "" || c.PendingRevision == nil || len(c.PendingRevision.ResourceIDs) != 2 {
		t.Fatal("partial collection snapshot", c)
	}
	pending := c.PendingRevision.ID
	if err = s.ReviewCollection(ctx, pending, "", true, ""); err != nil {
		t.Fatal(err)
	}
	c, err = s.Collection(ctx, owner, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	v := c.EditVersion
	in.ExpectedVersion = &v
	in.Name = "Pending title"
	in.ResourceIDs = []string{second}
	in.RepresentativeID = second
	changed, err := s.SubmitCollection(ctx, owner, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	live, err := s.PublicCollection(ctx, c.ID)
	if err != nil || live.Name != "Old live title" || len(live.Resources) != 2 || live.Description != "# body" {
		t.Fatal("pending snapshot leaked", live, err)
	}
	v = changed.EditVersion
	if _, err = s.SubmitCollection(ctx, owner, c.ID, in); !errors.Is(err, ErrConflict) {
		t.Fatal("pending replaced implicitly", err)
	}
	in.ReplacePending = changed.PendingRevision.ID
	newest, err := s.SubmitCollection(ctx, owner, c.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReviewCollection(ctx, changed.PendingRevision.ID, "", true, ""); !errors.Is(err, ErrConflict) {
		t.Fatal("superseded review approved", err)
	}
	if err = s.ReviewCollection(ctx, newest.PendingRevision.ID, "", true, ""); err != nil {
		t.Fatal(err)
	}
	live, err = s.PublicCollection(ctx, c.ID)
	if err != nil || live.Name != "Pending title" || len(live.Resources) != 1 {
		t.Fatal(live, err)
	}
	if _, err = db.Exec(`UPDATE resources SET moderation_state='suspended' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublicCollection(ctx, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("invisible member leaked", err)
	}
	invalid := in
	invalid.Slug = "should-not-exist"
	invalid.ReplacePending = ""
	invalid.ExpectedVersion = nil
	invalid.ResourceIDs = []string{uuid.NewString()}
	invalid.RepresentativeID = ""
	if _, err = s.SubmitCollection(ctx, owner, "", invalid); err == nil {
		t.Fatal("invalid member accepted")
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM resource_collections WHERE slug='should-not-exist'`).Scan(&count)
	if count != 0 {
		t.Fatal("failed submission created a partial collection")
	}
}
