package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestAdminHealthTemplateRequiresPreviewBeforeCleanup(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	err := NewTemplates().Render(recorder, "admin_health", map[string]any{
		"Title": "运行状态", "DBStatus": "ok", "DBLatency": "2ms", "Uptime": "5m0s",
		"Stats": model.Stats{}, "Diagnostics": store.AdminHealthDiagnostics{},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `action="/admin/cleanup/preview"`) {
		t.Fatal("health page is missing cleanup preview action")
	}
	if strings.Contains(body, `action="/admin/cleanup/execute"`) {
		t.Fatal("health page exposed cleanup execution before a preview")
	}
}

func TestAdminHealthTemplateShowsDangerousCleanupConfirmation(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	preview := store.AdminCleanupPreview{Cutoff: time.Now().UTC(), OAuthStates: 2, LoginTickets: 1}
	err := NewTemplates().Render(recorder, "admin_health", map[string]any{
		"Title": "运行状态", "DBStatus": "ok", "DBLatency": "2ms", "Uptime": "5m0s",
		"Stats": model.Stats{}, "Diagnostics": store.AdminHealthDiagnostics{},
		"CleanupPreview": preview, "CleanupToken": "signed-token", "CleanupConfirmation": "清理 3 条过期记录",
	})
	if err != nil {
		t.Fatal(err)
	}
	body := recorder.Body.String()
	for _, required := range []string{`action="/admin/cleanup/execute"`, `name="preview_token"`, `value="signed-token"`, "清理 3 条过期记录", "不可撤销"} {
		if !strings.Contains(body, required) {
			t.Fatalf("cleanup preview page is missing %q", required)
		}
	}
}
