package store

import "testing"

func TestAdminCommentQueryNormalized(t *testing.T) {
	q := (AdminCommentQuery{Search: " x ", State: " REVIEW ", Resource: " r ", User: " u ", Sort: "bad", Page: -1, PerPage: 999}).normalized()
	if q.Search != "x" || q.State != "review" || q.Resource != "r" || q.User != "u" {
		t.Fatalf("filters = %#v", q)
	}
	if q.Sort != "newest" || q.Page != 1 || q.PerPage != 100 {
		t.Fatalf("pagination = %#v", q)
	}
	if got := (AdminCommentQuery{State: "unknown"}).normalized().State; got != "" {
		t.Fatalf("invalid state = %q", got)
	}
}

func TestAdminCommentOrder(t *testing.T) {
	if got := adminCommentOrder("oldest"); got != "comment.created_at ASC,comment.id ASC" {
		t.Fatalf("order = %q", got)
	}
	if got := adminCommentOrder("invalid"); got != "comment.created_at DESC,comment.id DESC" {
		t.Fatalf("default order = %q", got)
	}
}
