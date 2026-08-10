package store

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestAdminCollectionQueryNormalization(t *testing.T) {
	t.Parallel()
	query := (AdminCollectionQuery{Search: "  suite ", Owner: " owner ", Kind: "invalid", State: "draft", Sort: "random", Page: -1, PerPage: 101}).normalized()
	if query.Search != "suite" || query.Owner != "owner" {
		t.Fatalf("text filters were not trimmed: %#v", query)
	}
	if query.Kind != "" || query.State != "" || query.Sort != "updated_desc" {
		t.Fatalf("invalid filters were not removed: %#v", query)
	}
	if query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("pagination was not normalized: %#v", query)
	}
}

func TestAdminCollectionQueryKeepsSupportedValues(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"quickapp", "watchface"} {
		for _, state := range []string{"pending", "approved", "rejected", "superseded"} {
			for _, sort := range []string{"updated_desc", "updated_asc", "created_desc", "name", "owner", "members_desc"} {
				query := (AdminCollectionQuery{Kind: kind, State: state, Sort: sort, Page: 2, PerPage: 50}).normalized()
				if query.Kind != kind || query.State != state || query.Sort != sort || query.Page != 2 || query.PerPage != 50 {
					t.Fatalf("supported query changed: %#v", query)
				}
			}
		}
	}
}

func TestAdminCollectionMetadataNormalization(t *testing.T) {
	t.Parallel()
	input, err := (AdminCollectionMetadataInput{Name: "  Collection ", Summary: " Summary "}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if input.Name != "Collection" || input.Summary != "Summary" {
		t.Fatalf("metadata was not trimmed: %#v", input)
	}
	invalid := []AdminCollectionMetadataInput{{}, {Name: string(make([]byte, 121))}, {Name: "valid", Summary: string(make([]byte, 4001))}}
	for _, candidate := range invalid {
		if _, err := candidate.normalized(); !errors.Is(err, ErrAdminResourceConflict) {
			t.Fatalf("invalid metadata error = %v, want conflict", err)
		}
	}
}

func TestAdminCollectionMetadataNormalizesOrderedSnapshot(t *testing.T) {
	t.Parallel()
	first, second := uuid.NewString(), uuid.NewString()
	input, err := (AdminCollectionMetadataInput{Name: "Suite", Enabled: true, ResourceIDs: []string{" " + first + " ", second}}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if input.RepresentativeResourceID != first || input.ResourceIDs[0] != first || !input.Enabled {
		t.Fatalf("snapshot not normalized: %#v", input)
	}
	for _, invalid := range []AdminCollectionMetadataInput{
		{Name: "Suite", ResourceIDs: []string{"invalid"}},
		{Name: "Suite", ResourceIDs: []string{first, first}},
		{Name: "Suite", ResourceIDs: []string{first}, RepresentativeResourceID: second},
	} {
		if _, err := invalid.normalized(); !errors.Is(err, ErrAdminResourceConflict) {
			t.Fatalf("invalid snapshot error=%v", err)
		}
	}
}

func TestAdminCollectionDTOsCarryRevisionAndMemberOrdering(t *testing.T) {
	t.Parallel()
	detail := AdminCollectionDetail{
		Collection: AdminCollectionItem{LatestRevisionState: "pending", MemberCount: 2},
		Revisions:  []AdminCollectionRevision{{Number: 2, State: "pending"}, {Number: 1, State: "approved"}},
		Members:    []AdminCollectionMember{{ID: "second", Position: 1}, {ID: "first", Position: 0}},
	}
	if detail.Collection.LatestRevisionState != "pending" || len(detail.Revisions) != 2 || detail.Revisions[0].Number != 2 {
		t.Fatalf("revision read model is incomplete: %#v", detail)
	}
	if detail.Members[0].Position == detail.Members[1].Position {
		t.Fatalf("member positions were not represented: %#v", detail.Members)
	}
}
