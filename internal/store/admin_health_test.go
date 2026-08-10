package store

import (
	"testing"
	"time"
)

func TestAdminCleanupPreviewTotal(t *testing.T) {
	t.Parallel()
	preview := AdminCleanupPreview{OAuthStates: 1, LoginTickets: 2, AdminSessions: 3, UserMessages: 4}
	if got := preview.Total(); got != 10 {
		t.Fatalf("Total() = %d, want 10", got)
	}
}

func TestAdminCleanupPreviewCutoffIsPreserved(t *testing.T) {
	t.Parallel()
	cutoff := time.Date(2026, 8, 10, 12, 34, 56, 789, time.FixedZone("test", 8*60*60))
	preview := AdminCleanupPreview{Cutoff: cutoff.UTC()}
	if !preview.Cutoff.Equal(cutoff) || preview.Cutoff.Location() != time.UTC {
		t.Fatalf("cutoff = %v, want UTC %v", preview.Cutoff, cutoff.UTC())
	}
}
