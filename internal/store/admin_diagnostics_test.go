package store

import (
	"strings"
	"testing"
	"time"
)

func TestAdminOAuthEventQueryNormalization(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)
	query := (AdminOAuthEventQuery{
		Search:   "  callback failed  ",
		App:      " oronbox ",
		Result:   " failure ",
		Platform: " android ",
		From:     &from,
		To:       &to,
		Page:     0,
		PerPage:  1000,
	}).normalized()
	if query.Search != "callback failed" || query.App != "oronbox" || query.Result != "failure" || query.Platform != "android" {
		t.Fatalf("text filters were not normalized: %#v", query)
	}
	if query.From != nil || query.To != nil {
		t.Fatalf("invalid time range was not discarded: %#v", query)
	}
	if query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("pagination was not normalized: %#v", query)
	}
}

func TestAdminDiagnosticResultNormalization(t *testing.T) {
	t.Parallel()
	if got := (AdminClientStatsQuery{Result: " failure "}).normalized().Result; got != "failure" {
		t.Fatalf("valid result = %q", got)
	}
	if got := (AdminOAuthEventQuery{Result: "maybe"}).normalized().Result; got != "" {
		t.Fatalf("invalid result = %q", got)
	}
}

func TestRedactDiagnosticText(t *testing.T) {
	t.Parallel()
	input := `exchange failed: access_token=abc.def&client_secret=hunter2 Authorization: Bearer very.secret token%3Dencoded`
	got := redactDiagnosticText(input)
	for _, secret := range []string{"abc.def", "hunter2", "very.secret", "encoded"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted text leaked %q: %s", secret, got)
		}
	}
	if count := strings.Count(got, "[REDACTED]"); count < 4 {
		t.Fatalf("redacted text has %d markers: %s", count, got)
	}
}

func TestAdminOAuthStateAndTicketStatusNormalization(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"active", "used", "expired"} {
		state := (AdminOAuthStateQuery{Status: status, Page: 2, PerPage: 50}).normalized()
		if state.Status != status || state.Page != 2 || state.PerPage != 50 {
			t.Fatalf("valid state query changed: %#v", state)
		}
		ticket := (AdminOAuthTicketQuery{Status: status, Page: 2, PerPage: 50}).normalized()
		if ticket.Status != status || ticket.Page != 2 || ticket.PerPage != 50 {
			t.Fatalf("valid ticket query changed: %#v", ticket)
		}
	}
	if query := (AdminOAuthStateQuery{Status: "pending"}).normalized(); query.Status != "" {
		t.Fatalf("invalid state status was retained: %#v", query)
	}
	if query := (AdminOAuthTicketQuery{Status: "pending"}).normalized(); query.Status != "" {
		t.Fatalf("invalid ticket status was retained: %#v", query)
	}
}

func TestAdminDiagnosticQueriesUseCommonPaginationDefaults(t *testing.T) {
	t.Parallel()
	queries := []struct {
		name    string
		page    int
		perPage int
	}{
		{"events", (AdminOAuthEventQuery{}).normalized().Page, (AdminOAuthEventQuery{}).normalized().PerPage},
		{"states", (AdminOAuthStateQuery{}).normalized().Page, (AdminOAuthStateQuery{}).normalized().PerPage},
		{"tickets", (AdminOAuthTicketQuery{}).normalized().Page, (AdminOAuthTicketQuery{}).normalized().PerPage},
		{"clients", (AdminClientStatsQuery{}).normalized().Page, (AdminClientStatsQuery{}).normalized().PerPage},
		{"audit", (AdminAuditLogQuery{}).normalized().Page, (AdminAuditLogQuery{}).normalized().PerPage},
	}
	for _, query := range queries {
		if query.page != 1 || query.perPage != 25 {
			t.Errorf("%s defaults = page %d, per-page %d", query.name, query.page, query.perPage)
		}
	}
}

func TestAdminDiagnosticTotalPages(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		total, perPage, want int
	}{{0, 25, 0}, {1, 25, 1}, {25, 25, 1}, {26, 25, 2}} {
		if got := adminDiagnosticTotalPages(test.total, test.perPage); got != test.want {
			t.Errorf("totalPages(%d, %d) = %d, want %d", test.total, test.perPage, got, test.want)
		}
	}
}

func TestAdminAuditQueryNormalizesReverseAssociationFilters(t *testing.T) {
	t.Parallel()
	query := (AdminAuditLogQuery{TargetType: " resource ", TargetID: " id-1 ", ActorUserID: " actor-1 "}).normalized()
	if query.TargetType != "resource" || query.TargetID != "id-1" || query.ActorUserID != "actor-1" {
		t.Fatalf("association filters = %#v", query)
	}
	if invalid := (AdminAuditLogQuery{TargetType: "secret"}).normalized(); invalid.TargetType != "" {
		t.Fatalf("invalid target type was retained: %#v", invalid)
	}
}
