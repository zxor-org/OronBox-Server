package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrReleaseNotFound = errors.New("app release not found")

type AppRelease struct {
	ID             string    `json:"id"`
	Version        string    `json:"version"`
	Channel        string    `json:"channel"`
	Platform       string    `json:"platform"`
	Arch           string    `json:"arch"`
	MinimumVersion string    `json:"minimum_version,omitempty"`
	NotesZH        string    `json:"notes_zh,omitempty"`
	NotesEN        string    `json:"notes_en,omitempty"`
	Tag            string    `json:"tag"`
	PublishedAt    time.Time `json:"published_at"`
}

func scanAppRelease(scanner interface{ Scan(...any) error }, item *AppRelease) error {
	return scanner.Scan(&item.ID, &item.Version, &item.Channel, &item.Platform, &item.Arch, &item.MinimumVersion, &item.NotesZH, &item.NotesEN, &item.Tag, &item.PublishedAt)
}

func (s *Store) PublishAppRelease(ctx context.Context, release AppRelease, actorID string) (AppRelease, error) {
	release.ID = uuid.NewString()
	if release.Tag == "" {
		release.Tag = release.Version
	}
	err := scanAppRelease(s.db.QueryRowContext(ctx, `INSERT INTO app_releases(id,version,channel,platform,arch,minimum_version,notes_zh,notes_en,tag,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text,version,channel,platform,arch,minimum_version,notes_zh,notes_en,tag,published_at`, release.ID, release.Version, release.Channel, release.Platform, release.Arch, release.MinimumVersion, release.NotesZH, release.NotesEN, release.Tag, actorID), &release)
	return release, err
}

func (s *Store) LatestAppRelease(ctx context.Context, channel, platform, arch string) (AppRelease, error) {
	var item AppRelease
	err := scanAppRelease(s.db.QueryRowContext(ctx, `SELECT id::text,version,channel,platform,arch,minimum_version,notes_zh,notes_en,tag,published_at FROM app_releases WHERE enabled AND source_available AND revoked_at IS NULL AND channel=$1 AND platform IN ($2,'all') AND arch IN ($3,'all') ORDER BY published_at DESC,(platform=$2) DESC,(arch=$3) DESC LIMIT 1`, channel, platform, arch), &item)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrReleaseNotFound
	}
	return item, err
}

func (s *Store) AppReleases(ctx context.Context) ([]AppRelease, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,version,channel,platform,arch,minimum_version,notes_zh,notes_en,tag,published_at FROM app_releases ORDER BY published_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AppRelease{}
	for rows.Next() {
		var item AppRelease
		if err := scanAppRelease(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type UpstreamRelease struct {
	ID          int64     `json:"id"`
	Tag         string    `json:"tag_name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
}

func (s *Store) SyncAppReleases(ctx context.Context, items []UpstreamRelease) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	ids := []int64{}
	for _, r := range items {
		if r.ID <= 0 || r.Tag == "" || r.Draft || r.Prerelease {
			continue
		}
		ids = append(ids, r.ID)
		var existing string
		// Adopt the legacy stable all-platform record, preserving its policy fields.
		if err = tx.QueryRowContext(ctx, `SELECT id::text FROM app_releases WHERE channel='stable' AND platform='all' AND arch='all' AND version=$1`, strings.TrimPrefix(r.Tag, "v")).Scan(&existing); err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE app_releases SET source_id=$2,tag=$3,source_available=true,notes_en=$4,published_at=$5,updated_at=now() WHERE id=$1`, existing, r.ID, r.Tag, r.Body, r.PublishedAt)
		} else if errors.Is(err, sql.ErrNoRows) {
			_, err = tx.ExecContext(ctx, `INSERT INTO app_releases(id,version,channel,platform,arch,source_id,tag,notes_en,published_at)VALUES($1,$2,'stable','all','all',$3,$4,$5,$6) ON CONFLICT(source_id)DO UPDATE SET version=excluded.version,tag=excluded.tag,notes_en=excluded.notes_en,published_at=excluded.published_at,source_available=true,updated_at=now()`, uuid.NewString(), strings.TrimPrefix(r.Tag, "v"), r.ID, r.Tag, r.Body, r.PublishedAt)
		}
		if err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE app_releases SET source_available=false,updated_at=now() WHERE source_id IS NOT NULL AND NOT(source_id=ANY($1))`, ids); err != nil {
		return err
	}
	return tx.Commit()
}
