package resources

import (
	"testing"
)

func TestAdminResourceQueryNormalization(t *testing.T) {
	t.Parallel()
	query := (AdminResourceQuery{
		Kind:              "firmware",
		Moderation:        "blocked",
		RevisionState:     "draft",
		ReviewState:       "unknown",
		PublicationTarget: "custom",
		PublicationState:  "queued",
		Sort:              "random",
		Page:              -1,
		PerPage:           1000,
	}).normalized()
	if query.Kind != "" || query.Moderation != "" || query.RevisionState != "" || query.ReviewState != "" || query.PublicationTarget != "" || query.PublicationState != "" {
		t.Fatalf("invalid filters were not discarded: %#v", query)
	}
	if query.Sort != "updated_desc" || query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("pagination was not normalized: %#v", query)
	}
}

func TestAdminResourceQueryKeepsGovernanceFilters(t *testing.T) {
	t.Parallel()
	query := (AdminResourceQuery{
		Kind:              "quickapp",
		Moderation:        "frozen",
		RevisionState:     "approved",
		ReviewState:       "approved",
		PublicationTarget: "astrobox",
		PublicationState:  "reviewing",
		Sort:              "owner",
		Page:              3,
		PerPage:           40,
	}).normalized()
	if query.Kind != "quickapp" || query.Moderation != "frozen" || query.RevisionState != "approved" || query.ReviewState != "approved" || query.PublicationTarget != "astrobox" || query.PublicationState != "reviewing" {
		t.Fatalf("valid filters changed: %#v", query)
	}
	if query.Sort != "owner" || query.Page != 3 || query.PerPage != 40 {
		t.Fatalf("valid pagination changed: %#v", query)
	}
}
