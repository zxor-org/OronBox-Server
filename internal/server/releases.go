package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminReleases(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminReleases(r.Context(), store.AdminReleaseQuery{Search: r.URL.Query().Get("q"), Platform: r.URL.Query().Get("platform"), Arch: r.URL.Query().Get("arch"), Channel: r.URL.Query().Get("channel"), Enabled: r.URL.Query().Get("state"), Sort: r.URL.Query().Get("sort"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_releases", map[string]any{"Title": "客户端版本", "Items": page.Items, "Page": page, "Query": page.Query, "Pager": web.NewPagination("/admin/releases", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action")})
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
	if err := store.ValidateAdminReleaseVersion(store.AdminReleaseVersionInput{Version: release.Version, MinimumVersion: release.MinimumVersion, Channel: release.Channel, Platform: release.Platform, Arch: release.Arch}); err != nil {
		_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "release.publish", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
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

func (a *App) handleAdminRelease(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.AdminRelease(r.Context(), r.PathValue("release"))
	if errors.Is(err, store.ErrAdminReleaseNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.render(w, "admin_release_detail", map[string]any{"Title": item.Version, "Item": item, "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminReleaseNotes(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	actor := currentAdmin(r)
	item, err := a.store.AdminUpdateReleaseNotes(r.Context(), r.PathValue("release"), store.AdminReleaseNotesInput{MinimumVersion: r.FormValue("minimum_version"), NotesZH: r.FormValue("notes_zh"), NotesEN: r.FormValue("notes_en")})
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "release.notes", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), 409)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "release.notes", "success", a.clientIP(r), r.UserAgent(), "release="+item.ID)
	http.Redirect(w, r, "/admin/releases/"+item.ID+"?action=notes", 302)
}

func (a *App) handleAdminReleaseState(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	actor := currentAdmin(r)
	action := r.FormValue("action")
	item, err := a.store.AdminSetReleaseState(r.Context(), r.PathValue("release"), action)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "release."+action, "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), 409)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "release."+action, "success", a.clientIP(r), r.UserAgent(), "release="+item.ID)
	http.Redirect(w, r, "/admin/releases/"+item.ID+"?action="+action, 302)
}
