package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdminBlogQueryNormalized(t *testing.T) {
	query := (AdminBlogQuery{Search: "  release notes  ", Published: " PUBLISHED ", Sort: "unknown", Page: -2, PerPage: 1000}).normalized()
	if query.Search != "release notes" || query.Published != "published" {
		t.Fatalf("normalized filters = %#v", query)
	}
	if query.Sort != "updated_desc" || query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("normalized pagination = %#v", query)
	}
	for _, state := range []string{"published", "draft"} {
		if got := (AdminBlogQuery{Published: state}).normalized().Published; got != state {
			t.Errorf("published filter %q normalized to %q", state, got)
		}
	}
	if got := (AdminBlogQuery{Published: "deleted"}).normalized().Published; got != "" {
		t.Fatalf("invalid published filter = %q", got)
	}
}

func TestAdminBlogOrder(t *testing.T) {
	tests := map[string]string{
		"updated_desc":   "post.updated_at DESC, post.slug DESC",
		"updated_asc":    "post.updated_at ASC, post.slug ASC",
		"created_desc":   "post.created_at DESC, post.slug DESC",
		"created_asc":    "post.created_at ASC, post.slug ASC",
		"published_desc": "post.published_at DESC NULLS LAST, post.slug DESC",
		"published_asc":  "post.published_at ASC NULLS LAST, post.slug ASC",
		"title_asc":      "post.title ASC, post.slug ASC",
		"title_desc":     "post.title DESC, post.slug DESC",
	}
	for input, want := range tests {
		if got := adminBlogOrder(input); got != want {
			t.Errorf("adminBlogOrder(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestAdminAnnouncementQueryNormalized(t *testing.T) {
	from := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	to := from.Add(-24 * time.Hour)
	query := (AdminAnnouncementQuery{Search: "  maintenance ", From: &from, To: &to, Page: 0, PerPage: 101}).normalized()
	if query.Search != "maintenance" || query.Page != 1 || query.PerPage != 100 {
		t.Fatalf("normalized query = %#v", query)
	}
	if query.From == nil || query.To == nil || !query.From.Equal(to) || !query.To.Equal(from) {
		t.Fatalf("normalized time range = %v - %v", query.From, query.To)
	}
}

func TestAdminContentPagination(t *testing.T) {
	if got := adminContentTotalPages(0, 25); got != 0 {
		t.Fatalf("zero total pages = %d", got)
	}
	if got := adminContentTotalPages(51, 25); got != 3 {
		t.Fatalf("total pages = %d", got)
	}
	page, perPage := normalizeAdminContentPage(-1, 0)
	if page != 1 || perPage != 25 {
		t.Fatalf("default pagination = %d/%d", page, perPage)
	}
}

func TestAdminBlogRejectsInvalidLookupAndAction(t *testing.T) {
	store := &Store{}
	if _, err := store.AdminBlogPost(context.Background(), " "); !errors.Is(err, ErrBlogPostNotFound) {
		t.Fatalf("AdminBlogPost error = %v", err)
	}
	if _, err := store.AdminSetBlogPostState(context.Background(), "post", "delete"); !errors.Is(err, ErrAdminBlogInvalid) {
		t.Fatalf("AdminSetBlogPostState invalid action error = %v", err)
	}
	if _, err := store.AdminSetBlogPostState(context.Background(), " ", "publish"); !errors.Is(err, ErrBlogPostNotFound) {
		t.Fatalf("AdminSetBlogPostState empty slug error = %v", err)
	}
}
