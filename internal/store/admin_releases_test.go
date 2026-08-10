package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAdminReleaseQueryNormalized(t *testing.T) {
	got := (AdminReleaseQuery{Search: "  2.0 ", Platform: " LINUX ", Arch: " ARM64 ", Channel: " BETA ", Enabled: "unknown", Sort: "unknown", Page: -2, PerPage: 1000}).normalized()
	if got.Search != "2.0" || got.Platform != "linux" || got.Arch != "arm64" || got.Channel != "beta" {
		t.Fatalf("normalized filters = %#v", got)
	}
	if got.Enabled != "" || got.Sort != "published_desc" || got.Page != 1 || got.PerPage != 100 {
		t.Fatalf("normalized defaults = %#v", got)
	}

	for _, state := range []string{"enabled", "disabled", "revoked"} {
		if value := (AdminReleaseQuery{Enabled: state}).normalized().Enabled; value != state {
			t.Errorf("enabled filter %q normalized to %q", state, value)
		}
	}
}

func TestAdminReleaseOrder(t *testing.T) {
	tests := map[string]string{
		"published_desc": "release.published_at DESC, release.id DESC",
		"published_asc":  "release.published_at ASC, release.id ASC",
		"version_desc":   "release.version DESC, release.published_at DESC, release.id DESC",
		"version_asc":    "release.version ASC, release.published_at ASC, release.id ASC",
		"updated_desc":   "release.updated_at DESC, release.id DESC",
		"updated_asc":    "release.updated_at ASC, release.id ASC",
	}
	for input, want := range tests {
		if got := adminReleaseOrder(input); got != want {
			t.Errorf("adminReleaseOrder(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateAdminReleaseVersion(t *testing.T) {
	valid := []AdminReleaseVersionInput{
		{Version: "1.2.3", Channel: "stable", Platform: "all", Arch: "all"},
		{Version: "v2.0.0-beta.2+build.4", MinimumVersion: "1.9.0", Channel: "beta", Platform: "linux", Arch: "arm64"},
		{Version: "3.0.0-nightly.20260810", MinimumVersion: "3.0.0-nightly.1", Channel: "nightly", Platform: "windows", Arch: "x86_64"},
	}
	for _, input := range valid {
		if err := ValidateAdminReleaseVersion(input); err != nil {
			t.Errorf("ValidateAdminReleaseVersion(%#v) = %v", input, err)
		}
	}

	invalid := []AdminReleaseVersionInput{
		{Version: "1.2", Channel: "stable", Platform: "all", Arch: "all"},
		{Version: "1.2.3-beta.01", Channel: "beta", Platform: "all", Arch: "all"},
		{Version: "18446744073709551616.0.0", Channel: "stable", Platform: "all", Arch: "all"},
		{Version: "1.2.3-beta.1", Channel: "stable", Platform: "all", Arch: "all"},
		{Version: "1.2.3", MinimumVersion: "2.0.0", Channel: "stable", Platform: "all", Arch: "all"},
		{Version: "1.2.3", Channel: "preview", Platform: "all", Arch: "all"},
		{Version: "1.2.3", Channel: "stable", Platform: "linux/amd64", Arch: "all"},
	}
	for _, input := range invalid {
		if err := ValidateAdminReleaseVersion(input); !errors.Is(err, ErrAdminReleaseInvalid) {
			t.Errorf("ValidateAdminReleaseVersion(%#v) error = %v", input, err)
		}
	}
}

func TestAdminReleaseNotesValidation(t *testing.T) {
	input, err := (AdminReleaseNotesInput{MinimumVersion: " v1.2.3 ", NotesZH: " 中文说明 ", NotesEN: " Notes "}).normalized()
	if err != nil {
		t.Fatal(err)
	}
	if input.MinimumVersion != "v1.2.3" || input.NotesZH != "中文说明" || input.NotesEN != "Notes" {
		t.Fatalf("normalized notes = %#v", input)
	}
	if _, err := (AdminReleaseNotesInput{MinimumVersion: "latest"}).normalized(); !errors.Is(err, ErrAdminReleaseInvalid) {
		t.Fatalf("invalid minimum version error = %v", err)
	}
}

func TestAdminReleaseState(t *testing.T) {
	item := AdminReleaseItem{Enabled: true}
	if item.State() != "enabled" {
		t.Fatalf("enabled state = %q", item.State())
	}
	item.Enabled = false
	if item.State() != "disabled" {
		t.Fatalf("disabled state = %q", item.State())
	}
	now := time.Now()
	item.Enabled, item.RevokedAt = true, &now
	if item.State() != "revoked" {
		t.Fatalf("revoked state = %q", item.State())
	}
}

func TestAdminReleaseRejectsInvalidID(t *testing.T) {
	store := &Store{}
	if _, err := store.AdminRelease(context.Background(), "bad-id"); !errors.Is(err, ErrAdminReleaseNotFound) {
		t.Fatalf("AdminRelease error = %v", err)
	}
	if _, err := store.AdminSetReleaseState(context.Background(), "bad-id", "enable"); !errors.Is(err, ErrAdminReleaseNotFound) {
		t.Fatalf("AdminSetReleaseState error = %v", err)
	}
}
