package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestReviewDetailPreselectsTheCurrentCurationGrade(t *testing.T) {
	// Approving a featured resource through a form that always defaults to
	// "standard" silently demotes it, which is why the grade is preselected.
	for _, grade := range []string{"standard", "featured"} {
		markup := renderReviewDetail(t, store.AdminReviewDetail{
			Review: store.AdminReviewItem{ID: "11111111-1111-1111-1111-111111111111", State: "pending", CurationGrade: grade},
		}, nil)
		selected := `<option value="` + grade + `" selected>`
		if !strings.Contains(markup, selected) {
			t.Fatalf("grade %q is not preselected in the decision form", grade)
		}
	}
}

func TestReviewDetailMarksTheRejectReasonAsRequired(t *testing.T) {
	markup := renderReviewDetail(t, store.AdminReviewDetail{
		Review: store.AdminReviewItem{ID: "11111111-1111-1111-1111-111111111111", State: "pending"},
	}, nil)
	if !strings.Contains(markup, `data-required-for="reject"`) {
		t.Fatal("the review note is not marked as required for a rejection")
	}
}

func TestReviewHistoryRendersEveryRecordedEvent(t *testing.T) {
	events := []store.AdminReviewEvent{
		{Event: "assigned", Actor: "alice", CreatedAt: time.Now()},
		{Event: "checklist_saved", Actor: "alice", Checklist: []string{"检查图标"}, CreatedAt: time.Now()},
		{Event: "rejected", Actor: "alice", Note: "截图模糊", CreatedAt: time.Now()},
	}
	markup := renderReviewDetail(t, store.AdminReviewDetail{
		Review: store.AdminReviewItem{ID: "11111111-1111-1111-1111-111111111111", State: "rejected"},
	}, events)
	for _, want := range []string{"已指派", "保存检查项", "退回修改", "检查图标", "截图模糊"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("review history is missing %q", want)
		}
	}
}

func TestReviewHistoryExplainsAnEmptyTimeline(t *testing.T) {
	markup := renderReviewDetail(t, store.AdminReviewDetail{
		Review: store.AdminReviewItem{ID: "11111111-1111-1111-1111-111111111111", State: "pending"},
	}, nil)
	if !strings.Contains(markup, "尚无处理记录") {
		t.Fatal("an empty review history should say so rather than render nothing")
	}
}

func renderReviewDetail(t *testing.T, detail store.AdminReviewDetail, events []store.AdminReviewEvent) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	data := map[string]any{
		"Title": "审核详情", "Detail": detail, "Attributes": nil,
		"Devices": nil, "Collections": nil, "Events": events,
	}
	if err := NewTemplates().Render(recorder, "admin_review_detail", data); err != nil {
		t.Fatalf("render admin_review_detail: %v", err)
	}
	return recorder.Body.String()
}
