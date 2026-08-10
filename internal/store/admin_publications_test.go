package store

import (
	"context"
	"errors"
	"testing"
)

func TestAdminPublicationQueryNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   AdminPublicationQuery
		want AdminPublicationQuery
	}{
		{
			name: "defaults and trims",
			in:   AdminPublicationQuery{Search: " publish ", Resource: " resource ", Owner: " owner "},
			want: AdminPublicationQuery{Search: "publish", Resource: "resource", Owner: "owner", Sort: "updated_desc", Page: 1, PerPage: 25},
		},
		{
			name: "preserves valid filters and paging",
			in:   AdminPublicationQuery{Target: "astrobox", State: "reviewing", Sort: "attempts_desc", Page: 3, PerPage: 50},
			want: AdminPublicationQuery{Target: "astrobox", State: "reviewing", Sort: "attempts_desc", Page: 3, PerPage: 50},
		},
		{
			name: "drops invalid enums and caps page size",
			in:   AdminPublicationQuery{Target: "unknown", State: "unknown", Sort: "random", Page: -3, PerPage: 1000},
			want: AdminPublicationQuery{Sort: "updated_desc", Page: 1, PerPage: 100},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.in.normalized(); got != tt.want {
				t.Fatalf("normalized query = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAdminPublicationOrder(t *testing.T) {
	tests := map[string]string{
		"updated_desc":  "p.updated_at DESC, p.id DESC",
		"updated_asc":   "p.updated_at ASC, p.id ASC",
		"created_desc":  "p.created_at DESC, p.id DESC",
		"created_asc":   "p.created_at ASC, p.id ASC",
		"attempts_desc": "p.attempts DESC, p.updated_at DESC, p.id DESC",
		"attempts_asc":  "p.attempts ASC, p.updated_at DESC, p.id DESC",
	}
	for sort, want := range tests {
		if got := adminPublicationOrder(sort); got != want {
			t.Errorf("adminPublicationOrder(%q) = %q, want %q", sort, got, want)
		}
	}
}

func TestAdminPublicationRejectsInvalidID(t *testing.T) {
	store := &Store{}
	_, err := store.AdminPublication(context.Background(), "not-a-uuid")
	if !errors.Is(err, ErrAdminPublicationNotFound) {
		t.Fatalf("AdminPublication error = %v, want %v", err, ErrAdminPublicationNotFound)
	}
}
