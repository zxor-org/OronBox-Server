package store

import (
	"errors"
	"testing"
	"time"
)

func TestAdminReviewQueryNormalization(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	query := (AdminReviewQuery{
		Search: "  resource ", Owner: " owner ", Kind: "invalid", Target: "other",
		State: "draft", Sort: "random", From: &from, To: &to, Page: -2, PerPage: 101,
	}).normalized()
	if query.Search != "resource" || query.Owner != "owner" {
		t.Fatalf("text filters were not trimmed: %#v", query)
	}
	if query.Kind != "" || query.Target != "" || query.State != "" || query.Sort != "updated_desc" {
		t.Fatalf("invalid enum filters were not cleared: %#v", query)
	}
	if query.From != nil || query.To != nil {
		t.Fatalf("inverted time range was not cleared: %#v", query)
	}
	if query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("pagination was not normalized: %#v", query)
	}
}

func TestNormalizeReviewChecklist(t *testing.T) {
	t.Parallel()
	got := normalizeReviewChecklist([]string{" preview ", "", "preview", " package "})
	want := []string{"preview", "package"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("normalizeReviewChecklist() = %#v, want %#v", got, want)
	}
}

func TestNormalizeReviewIDsRejectsUnsafeBatches(t *testing.T) {
	t.Parallel()
	if _, err := normalizeReviewIDs(nil); !errors.Is(err, ErrAdminReviewConflict) {
		t.Fatalf("empty batch error = %v", err)
	}
	if _, err := normalizeReviewIDs([]string{"not-a-uuid"}); !errors.Is(err, ErrAdminReviewNotFound) {
		t.Fatalf("invalid ID error = %v", err)
	}
}

func TestAdminReviewQueryKeepsSupportedValues(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"quickapp", "watchface"} {
		for _, target := range []string{"oronbox", "bandbbs", "astrobox"} {
			for _, state := range []string{"pending", "approved", "rejected", "superseded"} {
				for _, sort := range []string{"updated_desc", "updated_asc", "created_desc", "created_asc", "revision_desc", "owner"} {
					query := (AdminReviewQuery{Kind: kind, Target: target, State: state, Sort: sort, Page: 2, PerPage: 50}).normalized()
					if query.Kind != kind || query.Target != target || query.State != state || query.Sort != sort || query.Page != 2 || query.PerPage != 50 {
						t.Fatalf("supported query changed: %#v", query)
					}
				}
			}
		}
	}
}

func TestSummarizeAdminReviewDiff(t *testing.T) {
	t.Parallel()
	base := AdminReviewRevisionSnapshot{
		ID:   "base-revision",
		Name: "Old", Summary: "Summary", PaidType: "free",
		Attributes: []string{"original", "port"},
		Links:      []AdminLink{{Position: 0, Title: "Site", URL: "https://old.example"}},
		Media:      []AdminMedia{{ID: "old-media", SHA256: "aaa", Role: "preview", Position: 0}},
		Artifacts:  []AdminArtifact{{ID: "old-artifact", SHA256: "bbb", OriginalName: "old.rpk", Devices: []string{"Band 8"}}},
	}
	current := AdminReviewRevisionSnapshot{
		Name: "New", Summary: "Summary", PaidType: "paid",
		Attributes: []string{"original", "ai_generated"},
		Links:      []AdminLink{{Position: 0, Title: "Site", URL: "https://new.example"}},
		Media:      []AdminMedia{{ID: "new-media", SHA256: "ccc", Role: "preview", Position: 0}},
		Artifacts:  []AdminArtifact{{ID: "new-artifact", SHA256: "ddd", OriginalName: "new.rpk", Devices: []string{"Band 8", "Band 9"}}},
	}
	diff := summarizeAdminReviewDiff(base, current)
	if !diff.MetadataChanged || len(diff.MetadataFields) != 2 {
		t.Fatalf("metadata diff is incomplete: %#v", diff)
	}
	if diff.Attributes.Added != 1 || diff.Attributes.Removed != 1 || diff.Links.Changed != 1 {
		t.Fatalf("attribute/link diff is incomplete: %#v", diff)
	}
	if diff.Media.Changed != 1 || diff.Artifacts.Added != 1 || diff.Artifacts.Removed != 1 {
		t.Fatalf("asset diff is incomplete: %#v", diff)
	}
	if diff.Devices.Added != 1 || diff.Devices.Removed != 0 {
		t.Fatalf("device diff is incomplete: %#v", diff)
	}
	if len(diff.Metadata) != 2 || diff.Metadata[0].Before == "" || diff.Metadata[0].After == "" {
		t.Fatalf("field-level metadata diff is incomplete: %#v", diff.Metadata)
	}
	if len(diff.MediaItems) != 1 || diff.MediaItems[0].Change != "changed" {
		t.Fatalf("field-level media diff is incomplete: %#v", diff.MediaItems)
	}
	if len(diff.DeviceItems) != 1 || diff.DeviceItems[0].Change != "added" {
		t.Fatalf("field-level device diff is incomplete: %#v", diff.DeviceItems)
	}
}

func TestSummarizeAdminReviewDiffWithoutBaseMarksCurrentAsAdded(t *testing.T) {
	t.Parallel()
	current := AdminReviewRevisionSnapshot{
		Name: "First", Attributes: []string{"original"},
		Links:     []AdminLink{{Position: 0, Title: "Site", URL: "https://example.com"}},
		Media:     []AdminMedia{{SHA256: "aaa", Role: "preview", Position: 0}},
		Artifacts: []AdminArtifact{{SHA256: "bbb", OriginalName: "app.rpk", Devices: []string{"Band 9"}}},
	}
	diff := summarizeAdminReviewDiff(AdminReviewRevisionSnapshot{}, current)
	if diff.HasBase || diff.Attributes.Added != 1 || diff.Links.Added != 1 || diff.Media.Added != 1 || diff.Artifacts.Added != 1 || diff.Devices.Added != 1 {
		t.Fatalf("first revision diff is incomplete: %#v", diff)
	}
}
