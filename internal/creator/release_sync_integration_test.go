package creator

import (
	"context"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"testing"
	"time"
)

func TestReleaseSyncPreservesPolicyAndWithdrawsMissingUpstream(t *testing.T) {
	db := testDatabase(t)
	s := store.New(db)
	ctx := context.Background()
	releases := []store.UpstreamRelease{{ID: 71, Tag: "v1.2.0", Body: "upstream", PublishedAt: time.Now().UTC()}}
	if err := s.SyncAppReleases(ctx, releases); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE app_releases SET enabled=false,revoked_at=now(),minimum_version='1.1.9',notes_zh='人工说明' WHERE source_id=71`); err != nil {
		t.Fatal(err)
	}
	releases[0].Body = "upstream refreshed"
	if err := s.SyncAppReleases(ctx, releases); err != nil {
		t.Fatal(err)
	}
	var enabled, revoked, available bool
	var min, zh, en string
	var count int
	if err := db.QueryRow(`SELECT enabled,revoked_at IS NOT NULL,source_available,minimum_version,notes_zh,notes_en FROM app_releases WHERE source_id=71`).Scan(&enabled, &revoked, &available, &min, &zh, &en); err != nil {
		t.Fatal(err)
	}
	if enabled || !revoked || !available || min != "1.1.9" || zh != "人工说明" || en != "upstream refreshed" {
		t.Fatal(enabled, revoked, available, min, zh, en)
	}
	if err := db.QueryRow(`SELECT count(*) FROM app_releases WHERE source_id=71`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if err := s.SyncAppReleases(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT source_available FROM app_releases WHERE source_id=71`).Scan(&available); err != nil || available {
		t.Fatal(available, err)
	}
}
