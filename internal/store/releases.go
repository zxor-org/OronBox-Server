package store

import (
	"context"
	"database/sql"
	"errors"
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
	DownloadURL    string    `json:"download_url"`
	PublishedAt    time.Time `json:"published_at"`
}

func scanAppRelease(scanner interface{ Scan(...any) error }, item *AppRelease) error {
	return scanner.Scan(&item.ID, &item.Version, &item.Channel, &item.Platform, &item.Arch, &item.MinimumVersion, &item.NotesZH, &item.NotesEN, &item.DownloadURL, &item.PublishedAt)
}

func (s *Store) PublishAppRelease(ctx context.Context, release AppRelease, actorID string) (AppRelease, error) {
	release.ID = uuid.NewString()
	err := scanAppRelease(s.db.QueryRowContext(ctx, `INSERT INTO app_releases(id,version,channel,platform,arch,minimum_version,notes_zh,notes_en,download_url,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text,version,channel,platform,arch,minimum_version,notes_zh,notes_en,download_url,published_at`, release.ID, release.Version, release.Channel, release.Platform, release.Arch, release.MinimumVersion, release.NotesZH, release.NotesEN, release.DownloadURL, actorID), &release)
	return release, err
}

func (s *Store) LatestAppRelease(ctx context.Context, channel, platform, arch string) (AppRelease, error) {
	var item AppRelease
	err := scanAppRelease(s.db.QueryRowContext(ctx, `SELECT id::text,version,channel,platform,arch,minimum_version,notes_zh,notes_en,download_url,published_at FROM app_releases WHERE channel=$1 AND platform IN ($2,'all') AND arch IN ($3,'all') ORDER BY published_at DESC,(platform=$2) DESC,(arch=$3) DESC LIMIT 1`, channel, platform, arch), &item)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrReleaseNotFound
	}
	return item, err
}

func (s *Store) AppReleases(ctx context.Context) ([]AppRelease, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,version,channel,platform,arch,minimum_version,notes_zh,notes_en,download_url,published_at FROM app_releases ORDER BY published_at DESC LIMIT 100`)
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
