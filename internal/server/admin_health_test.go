package server

import (
	"strings"
	"testing"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestCleanupPreviewTokenIsBoundAndExpires(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	app := &App{cfg: config.Config{EncryptionKey: "health-test-key-that-is-long-enough"}}
	want := store.AdminCleanupPreview{Cutoff: now.Add(-time.Second), OAuthStates: 2, LoginTickets: 3, AdminSessions: 4, UserMessages: 5}
	token, err := app.signCleanupPreview(cleanupPreviewToken{Preview: want, ActorID: "admin-a", ExpiresAt: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	got, err := app.verifyCleanupPreview(token, "admin-a", now)
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if got.Total() != want.Total() || !got.Cutoff.Equal(want.Cutoff) {
		t.Fatalf("preview changed: got %#v want %#v", got, want)
	}
	if _, err := app.verifyCleanupPreview(token, "admin-b", now); err == nil {
		t.Fatal("preview token was accepted for another administrator")
	}
	if _, err := app.verifyCleanupPreview(token, "admin-a", now.Add(2*time.Minute)); err == nil {
		t.Fatal("expired preview token was accepted")
	}
}

func TestCleanupPreviewTokenRejectsTampering(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	app := &App{cfg: config.Config{EncryptionKey: "health-test-key-that-is-long-enough"}}
	token, err := app.signCleanupPreview(cleanupPreviewToken{
		Preview: store.AdminCleanupPreview{Cutoff: now.Add(-time.Second), OAuthStates: 1},
		ActorID: "admin-a", ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[0] = "A" + parts[0][1:]
	if _, err := app.verifyCleanupPreview(strings.Join(parts, "."), "admin-a", now); err == nil {
		t.Fatal("tampered cleanup preview token was accepted")
	}
}

func TestCleanupConfirmationIncludesExactPreviewTotal(t *testing.T) {
	t.Parallel()
	preview := store.AdminCleanupPreview{OAuthStates: 2, LoginTickets: 3, AdminSessions: 4, UserMessages: 5}
	if got, want := cleanupConfirmation(preview), "清理 14 条过期记录"; got != want {
		t.Fatalf("confirmation = %q, want %q", got, want)
	}
}
