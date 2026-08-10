package store

import (
	"context"
	"errors"
	"testing"
)

func TestAdminBlobQueryNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   AdminBlobQuery
		want AdminBlobQuery
	}{
		{
			name: "defaults and trims",
			in:   AdminBlobQuery{Search: " digest ", MediaType: " IMAGE/PNG "},
			want: AdminBlobQuery{Search: "digest", MediaType: "image/png", Sort: "created_desc", Page: 1, PerPage: 25},
		},
		{
			name: "preserves filters and paging",
			in:   AdminBlobQuery{ReplicaState: "failed", Referenced: "referenced", Sort: "size_desc", Page: 4, PerPage: 50},
			want: AdminBlobQuery{ReplicaState: "failed", Referenced: "referenced", Sort: "size_desc", Page: 4, PerPage: 50},
		},
		{
			name: "supports missing replica and unreferenced blobs",
			in:   AdminBlobQuery{ReplicaState: "MISSING", Referenced: "UNREFERENCED", Sort: "sha256"},
			want: AdminBlobQuery{ReplicaState: "missing", Referenced: "unreferenced", Sort: "sha256", Page: 1, PerPage: 25},
		},
		{
			name: "drops invalid enums and caps page size",
			in:   AdminBlobQuery{ReplicaState: "deleted", Referenced: "maybe", Sort: "random", Page: -2, PerPage: 1000},
			want: AdminBlobQuery{Sort: "created_desc", Page: 1, PerPage: 100},
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

func TestAdminBlobOrder(t *testing.T) {
	tests := map[string]string{
		"created_desc": "blob.created_at DESC, blob.sha256 DESC",
		"created_asc":  "blob.created_at ASC, blob.sha256 ASC",
		"size_desc":    "blob.size_bytes DESC, blob.created_at DESC, blob.sha256 DESC",
		"size_asc":     "blob.size_bytes ASC, blob.created_at DESC, blob.sha256 DESC",
		"media_type":   "lower(blob.media_type), blob.created_at DESC, blob.sha256 DESC",
		"sha256":       "blob.sha256 ASC",
	}
	for sort, want := range tests {
		if got := adminBlobOrder(sort); got != want {
			t.Errorf("adminBlobOrder(%q) = %q, want %q", sort, got, want)
		}
	}
}

func TestAdminBlobRejectsInvalidDigestWithoutDatabase(t *testing.T) {
	store := &Store{}
	if _, err := store.AdminBlob(context.Background(), "not-a-digest"); !errors.Is(err, ErrAdminBlobNotFound) {
		t.Fatalf("AdminBlob error = %v, want %v", err, ErrAdminBlobNotFound)
	}
	if _, err := store.AdminRequeueBlobReplica(context.Background(), "not-a-digest"); !errors.Is(err, ErrAdminBlobNotFound) {
		t.Fatalf("AdminRequeueBlobReplica error = %v, want %v", err, ErrAdminBlobNotFound)
	}
}
