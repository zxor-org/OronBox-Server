package server

import (
	"net/http"
	"strings"
	"testing"
)

// Hiding a comment is what an author will dispute, so both the single and the
// batch path have to record why. The rule is enforced on the server because
// the browser check is only there to save a round trip.
func TestRejectingACollectionRequiresAReason(t *testing.T) {
	recorder := performAdminRequest(t, adminPermissionTestApp(t, "admin"), http.MethodPost, "/admin/api/collections/review/revision-1", `{"approve":false}`, "https://admin.example")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "理由") {
		t.Errorf("the rejection does not say a reason is required: %q", recorder.Body.String())
	}
}

func TestHidingACommentRequiresAReason(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "single", path: "/admin/api/comments/00000000-0000-0000-0000-000000000001", body: `{"action":"hide"}`},
		{name: "batch", path: "/admin/comments/bulk", body: "bulk_action=hide&comment_ids=00000000-0000-0000-0000-000000000001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := performAdminRequest(t, adminPermissionTestApp(t, "admin"), http.MethodPost, test.path, test.body, "https://admin.example")
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%q", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "理由") {
				t.Errorf("the rejection does not say a reason is required: %q", recorder.Body.String())
			}
		})
	}
}

// Approving needs no reason, so the same request without a note must get past
// validation rather than being blocked by an over-broad rule.
func TestApprovingACommentNeedsNoReason(t *testing.T) {
	recorder := performAdminRequest(t, adminPermissionTestApp(t, "admin"), http.MethodPost, "/admin/comments/bulk", "bulk_action=approve&comment_ids=00000000-0000-0000-0000-000000000001", "https://admin.example")
	if recorder.Code == http.StatusBadRequest && strings.Contains(recorder.Body.String(), "理由") {
		t.Fatalf("approving was blocked for a missing reason: %q", recorder.Body.String())
	}
}

// A reviewer moderates comments, so the batch endpoint must not be quietly
// admin-only; that would push them back to clicking one comment at a time.
func TestReviewersCanUseTheCommentBatchEndpoint(t *testing.T) {
	recorder := performAdminRequest(t, adminPermissionTestApp(t, "reviewer"), http.MethodPost, "/admin/comments/bulk", "bulk_action=approve&comment_ids=00000000-0000-0000-0000-000000000001", "https://admin.example")
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("a reviewer was refused the comment batch endpoint: %q", recorder.Body.String())
	}
}

// The batch route is a literal sibling of the /{comment} pattern, so this
// pins down that it is not swallowed as a comment id.
func TestCommentBatchRouteIsNotTreatedAsACommentID(t *testing.T) {
	recorder := performAdminRequest(t, adminPermissionTestApp(t, "admin"), http.MethodPost, "/admin/comments/bulk", "bulk_action=hide&comment_ids=00000000-0000-0000-0000-000000000001", "https://admin.example")
	if body := recorder.Body.String(); strings.Contains(body, "comment was not found") {
		t.Fatalf("the batch route was routed to the single-comment handler: %q", body)
	}
}
