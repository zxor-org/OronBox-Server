package store

import (
	"context"
	"errors"
	"testing"
)

func TestAdminFeedbackQueryNormalization(t *testing.T) {
	t.Parallel()
	query := (AdminFeedbackQuery{Kind: "abuse", Status: "queued", Page: 0, PerPage: 101}).normalized()
	if query.Kind != "" || query.Status != "" || query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("unexpected query: %#v", query)
	}
	for _, status := range []string{"open", "investigating", "replied", "resolved", "dismissed", "closed"} {
		if !validFeedbackStatus(status) {
			t.Fatalf("expected %q to be valid", status)
		}
	}
	if validFeedbackStatus("pending") {
		t.Fatal("unexpected pending feedback status")
	}
}

func TestFeedbackTargetValidationDoesNotQueryMalformedLocalID(t *testing.T) {
	t.Parallel()
	found, err := (&Store{}).FeedbackTargetExists(context.Background(), "oronBox", "not-a-uuid")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("malformed local resource ID unexpectedly exists")
	}
	found, err = (&Store{}).FeedbackTargetExists(context.Background(), "astroboxRepo", "external-resource-id")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("external resource target was rejected before external review")
	}
}

func TestGovernanceRoutesTreatMalformedIDsAsMissing(t *testing.T) {
	t.Parallel()
	databaseFreeStore := &Store{}
	if _, err := databaseFreeStore.AdminResource(context.Background(), "not-a-uuid"); !errors.Is(err, ErrAdminResourceNotFound) {
		t.Fatalf("AdminResource malformed ID error = %v", err)
	}
	if _, err := databaseFreeStore.AdminManageResource(context.Background(), "not-a-uuid", "suspend", "", AdminSession{}); !errors.Is(err, ErrAdminResourceNotFound) {
		t.Fatalf("AdminManageResource malformed ID error = %v", err)
	}
	if _, err := databaseFreeStore.Feedback(context.Background(), "not-a-uuid", "", true); !errors.Is(err, ErrFeedbackNotFound) {
		t.Fatalf("Feedback malformed ID error = %v", err)
	}
	if _, err := databaseFreeStore.UpdateFeedback(context.Background(), "not-a-uuid", FeedbackUpdate{Status: "open"}); !errors.Is(err, ErrFeedbackNotFound) {
		t.Fatalf("UpdateFeedback malformed ID error = %v", err)
	}
}
