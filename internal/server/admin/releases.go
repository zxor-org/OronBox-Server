package admin

import (
	"errors"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIReleases(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminReleases(r.Context(), store.AdminReleaseQuery{
		Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("releases_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "version": item.Version, "minimum_version": item.MinimumVersion,
			"channel": item.Channel, "platform": item.Platform, "arch": item.Arch,
			"tag": item.Tag, "published_at": item.PublishedAt,
			"enabled": item.Enabled, "revoked_at": item.RevokedAt, "state": item.State(),
			"updated_at": item.UpdatedAt, "created_by": item.CreatedBy, "creator": item.Creator,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "releases_failed")
}

func (a *Handler) HandleAdminAPIRelease(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.AdminRelease(r.Context(), r.PathValue("release"))
	if err != nil {
		if errors.Is(err, store.ErrAdminReleaseNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("release_not_found", "app release was not found"))
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("releases_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"id": item.ID, "version": item.Version, "minimum_version": item.MinimumVersion,
		"channel": item.Channel, "platform": item.Platform, "arch": item.Arch,
		"tag": item.Tag, "published_at": item.PublishedAt,
		"enabled": item.Enabled, "revoked_at": item.RevokedAt, "state": item.State(),
		"updated_at": item.UpdatedAt, "created_by": item.CreatedBy, "creator": item.Creator,
		"notes_zh": item.NotesZH, "notes_en": item.NotesEN,
	})
}

func (a *Handler) HandleAdminAPIReleasePublish(w http.ResponseWriter, r *http.Request) {
	var release store.AppRelease
	if err := httpx.ReadJSON(r, &release); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	created, err := a.store.PublishAppRelease(r.Context(), release, a.actor(r).UserID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("release_failed", err.Error()))
		return
	}
	a.audit(r, "release.publish", "success", "version="+created.Version)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "id": created.ID})
}

func (a *Handler) HandleAdminAPIReleaseNotes(w http.ResponseWriter, r *http.Request) {
	var input store.AdminReleaseNotesInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	item, err := a.store.AdminUpdateReleaseNotes(r.Context(), r.PathValue("release"), input)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("release_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (a *Handler) HandleAdminAPIReleaseStateJSON(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	item, err := a.store.AdminSetReleaseState(r.Context(), r.PathValue("release"), body.Action)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("release_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}
