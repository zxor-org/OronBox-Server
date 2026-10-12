package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

func (a *application) runReleaseSyncWorker(ctx context.Context) {
	// Keep the client version feed fresh even when no administrator visits the
	// console. A failed poll is retried on the next interval and never deletes
	// the last known good release rows.
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		_, _ = a.syncReleases(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *application) releaseSync(w http.ResponseWriter, r *http.Request) {
	synced, err := a.syncReleases(r.Context())
	if err != nil {
		if strings.Contains(err.Error(), "config") {
			httputil.Error(w, http.StatusInternalServerError, "release_sync_config_invalid", "客户端版本源配置无效")
		} else if strings.Contains(err.Error(), "读取") {
			httputil.Error(w, http.StatusBadGateway, "release_sync_failed", "读取客户端版本源失败")
		} else {
			httputil.Error(w, http.StatusInternalServerError, "release_sync_failed", "保存客户端版本失败")
		}
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"status": "synced", "synced": synced})
}

func (a *application) syncReleases(ctx context.Context) (int, error) {
	started := time.Now()
	owner, repo, err := syndication.SplitRepository(a.cfg.GitHubReleaseRepo)
	if err != nil {
		return 0, fmt.Errorf("release sync config: %w", err)
	}
	client := syndication.GitHubClient{BaseURL: a.cfg.GitHub.APIURL, Token: a.cfg.GitHubReleaseToken}
	upstream, err := client.ListReleases(ctx, owner, repo)
	if err != nil {
		a.logger.Warn("github release sync failed", "repo", a.cfg.GitHubReleaseRepo, "duration_ms", time.Since(started).Milliseconds(), "error", observability.SafeError(err))
		return 0, fmt.Errorf("读取客户端版本源: %w", err)
	}
	if a.db == nil || a.db.Pool == nil {
		return 0, fmt.Errorf("database unavailable")
	}
	synced := 0
	err = a.db.WithTx(ctx, func(tx pgx.Tx) error {
		for _, release := range upstream {
			if release.Draft || release.Prerelease {
				continue
			}
			parsed, parseErr := syndication.ParseAppRelease(release)
			if parseErr != nil {
				continue
			}
			if _, execErr := tx.Exec(ctx, `
				INSERT INTO app_releases (version, minimum_version, notes_zh, notes_en, published_at, synced_at)
				VALUES ($1, $2, $3, $4, $5, now())
				ON CONFLICT (version) DO UPDATE SET
					minimum_version = EXCLUDED.minimum_version,
					notes_zh = EXCLUDED.notes_zh,
					notes_en = EXCLUDED.notes_en,
					published_at = EXCLUDED.published_at,
					synced_at = now()
			`, parsed.Version, parsed.MinimumVersion, parsed.NotesZH, parsed.NotesEN, parsed.PublishedAt); execErr != nil {
				return execErr
			}
			synced++
		}
		return nil
	})
	if err != nil {
		a.logger.Error("github release sync database write failed", "repo", a.cfg.GitHubReleaseRepo, "duration_ms", time.Since(started).Milliseconds(), "error", observability.SafeError(err))
		return 0, fmt.Errorf("保存客户端版本: %w", err)
	}
	a.logger.Info("github release sync completed", "repo", a.cfg.GitHubReleaseRepo, "synced", synced, "duration_ms", time.Since(started).Milliseconds())
	return synced, nil
}
