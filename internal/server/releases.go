package server

import (
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminReleases(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.AppReleases(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_releases", map[string]any{"Title": "客户端版本", "Items": items})
}

func (a *App) handleAdminPublishRelease(w http.ResponseWriter, r *http.Request) {
	release := store.AppRelease{Version: strings.TrimSpace(r.FormValue("version")), Channel: strings.TrimSpace(r.FormValue("channel")), Platform: strings.TrimSpace(r.FormValue("platform")), Arch: strings.TrimSpace(r.FormValue("arch")), MinimumVersion: strings.TrimSpace(r.FormValue("minimum_version")), NotesZH: strings.TrimSpace(r.FormValue("notes_zh")), NotesEN: strings.TrimSpace(r.FormValue("notes_en")), DownloadURL: strings.TrimSpace(r.FormValue("download_url"))}
	if release.Channel == "" {
		release.Channel = "stable"
	}
	if release.Platform == "" {
		release.Platform = "all"
	}
	if release.Arch == "" {
		release.Arch = "all"
	}
	if release.Version == "" || release.DownloadURL == "" {
		http.Error(w, "version and download URL are required", http.StatusBadRequest)
		return
	}
	created, err := a.store.PublishAppRelease(r.Context(), release, currentAdmin(r).UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "release.publish", "success", a.clientIP(r), r.UserAgent(), "release="+created.Version+" channel="+created.Channel)
	http.Redirect(w, r, "/admin/releases", http.StatusFound)
}
