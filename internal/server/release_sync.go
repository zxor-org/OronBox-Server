package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) syncReleases(ctx context.Context) (synced int, syncErr error) {
	if !a.releaseSyncMu.TryLock() {
		return 0, fmt.Errorf("release sync already running")
	}
	defer a.releaseSyncMu.Unlock()
	started := time.Now().UTC()
	defer func() {
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = a.store.UpdateSetting(saveCtx, "sync.releases", func(raw string) (string, error) {
			value := map[string]any{}
			if raw != "" {
				_ = json.Unmarshal([]byte(raw), &value)
			}
			value["last_attempt"] = started
			if syncErr == nil {
				value["last_success"] = time.Now().UTC()
				value["synced_count"] = synced
				value["last_error"] = ""
			} else {
				value["last_error"] = syncErr.Error()
			}
			return store.EncodeSetting(value)
		})
	}()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	all := []store.UpstreamRelease{}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for page := 1; page <= 50; page++ {
		req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/repos/zxor-org/oronbox/releases?per_page=100&page=%d", a.cfg.GitHub.APIURL, page), nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		res, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return 0, fmt.Errorf("GitHub release sync HTTP %d", res.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
		res.Body.Close()
		if err != nil || len(data) > 8<<20 {
			return 0, fmt.Errorf("release response too large")
		}
		var batch []store.UpstreamRelease
		if json.Unmarshal(data, &batch) != nil {
			return 0, fmt.Errorf("invalid GitHub release response")
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			if err = a.store.SyncAppReleases(ctx, all); err != nil {
				return 0, err
			}
			return len(all), nil
		}
	}
	return 0, fmt.Errorf("GitHub release pagination exceeded")
}

func (a *App) RunReleaseSync(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		n, err := a.syncReleases(ctx)
		if err != nil && ctx.Err() == nil {
			oauthLog(ctx).Warn("GitHub release sync failed", "error", err)
		} else if err == nil {
			oauthLog(ctx).Info("GitHub release sync completed", "synced", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) handleReleaseSync(w http.ResponseWriter, r *http.Request) {
	n, err := a.syncReleases(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("release_sync_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"synced": n})
}

func (a *App) handleReleaseSyncStatus(w http.ResponseWriter, r *http.Request) {
	raw, err := a.store.Setting(r.Context(), "sync.releases", "{}")
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("status_failed", "release sync status unavailable"))
		return
	}
	var value map[string]any
	if json.Unmarshal([]byte(raw), &value) != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("status_failed", "release sync status invalid"))
		return
	}
	httpx.WriteJSON(w, 200, value)
}
