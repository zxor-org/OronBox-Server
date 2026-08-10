package store

import (
	"context"
	"errors"
	"testing"
)

func TestAdminPluginQueryNormalized(t *testing.T) {
	tests := []struct {
		name string
		in   AdminPluginQuery
		want AdminPluginQuery
	}{
		{
			name: "defaults and trims",
			in:   AdminPluginQuery{Search: " plugin ", Uploader: " owner "},
			want: AdminPluginQuery{Search: "plugin", Uploader: "owner", Sort: "updated_desc", Page: 1, PerPage: 25},
		},
		{
			name: "keeps complete filters",
			in:   AdminPluginQuery{State: "LISTED", Runtime: "WASM", Sort: "size_desc", Page: 3, PerPage: 50},
			want: AdminPluginQuery{State: "listed", Runtime: "wasm", Sort: "size_desc", Page: 3, PerPage: 50},
		},
		{
			name: "drops invalid enums and caps paging",
			in:   AdminPluginQuery{State: "hidden", Runtime: "native", Sort: "random", Page: -1, PerPage: 1000},
			want: AdminPluginQuery{Sort: "updated_desc", Page: 1, PerPage: 100},
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

func TestAdminPluginOrder(t *testing.T) {
	tests := map[string]string{
		"updated_desc": "p.updated_at DESC, p.id ASC",
		"updated_asc":  "p.updated_at ASC, p.id ASC",
		"created_desc": "p.created_at DESC, p.id ASC",
		"created_asc":  "p.created_at ASC, p.id ASC",
		"name":         "lower(p.name), p.id ASC",
		"uploader":     "lower(COALESCE(u.username,'')), lower(p.name), p.id ASC",
		"size_desc":    "COALESCE(package_blob.size_bytes,0) DESC, p.updated_at DESC, p.id ASC",
		"size_asc":     "COALESCE(package_blob.size_bytes,0) ASC, p.updated_at DESC, p.id ASC",
	}
	for sort, want := range tests {
		if got := adminPluginOrder(sort); got != want {
			t.Errorf("adminPluginOrder(%q) = %q, want %q", sort, got, want)
		}
	}
}

func TestAdminPluginV2RejectsEmptyID(t *testing.T) {
	store := &Store{}
	_, err := store.AdminPluginV2(context.Background(), "  ")
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("AdminPluginV2 error = %v, want %v", err, ErrPluginNotFound)
	}
}

func TestAdminPluginMetadataRevisionInputValidation(t *testing.T) {
	input, err := (AdminPluginMetadataRevisionInput{Name: " Renamed ", Author: " Author ", Description: " Text "}).normalized()
	if err != nil || input.Name != "Renamed" || input.Author != "Author" || input.Description != "Text" {
		t.Fatalf("normalized input = %#v, error = %v", input, err)
	}
	if _, err := (AdminPluginMetadataRevisionInput{}).normalized(); err == nil {
		t.Fatal("empty plugin metadata was accepted")
	}
}
