package server

import (
	"net/http/httptest"
	"testing"
)

func TestAdminAuditQueryKeepsCSVAndListFiltersAligned(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("GET", "/admin/audit.csv?q=blob.read&result=success&target_type=blob&target_id=abc&actor_user_id=actor&from=2026-08-01&to=2026-08-10&page=7&per_page=50", nil)
	query := adminAuditQuery(request)
	if query.Search != "blob.read" || query.Result != "success" || query.TargetType != "blob" || query.TargetID != "abc" || query.ActorUserID != "actor" || query.Page != 7 || query.PerPage != 50 || query.From == nil || query.To == nil {
		t.Fatalf("query = %#v", query)
	}
}

func TestAuditCSVJSON(t *testing.T) {
	t.Parallel()
	if got := auditCSVJSON(nil); got != "" {
		t.Fatalf("nil JSON = %q", got)
	}
	if got := auditCSVJSON(map[string]any{"state": "visible"}); got != `{"state":"visible"}` {
		t.Fatalf("JSON = %q", got)
	}
}

func TestAuditCSVCellsCannotBecomeSpreadsheetFormulas(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"=1+1", "+cmd", "-2+3", "@SUM(A1:A2)", "\tformula", "\rformula"} {
		if got := safeCSVCell(value); got != "'"+value {
			t.Errorf("safeCSVCell(%q) = %q", value, got)
		}
	}
	if got := safeCSVCell("normal"); got != "normal" {
		t.Fatalf("normal cell changed to %q", got)
	}
}
