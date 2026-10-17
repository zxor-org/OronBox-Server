package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIPlugins(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminPluginsV2(r.Context(), store.AdminPluginQuery{
		Search: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("plugins_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		state := item.State
		if item.PendingVersionID != "" {
			state = "pending"
		}
		items = append(items, map[string]any{
			"id": item.ID, "name": item.Name, "state": state, "version": item.Version,
			"author": item.Author, "uploader_id": item.UploaderID, "uploader": item.UploaderName,
			"runtime": item.Runtime, "package_size": item.PackageSize, "icon_sha256": item.IconSHA256,
			"package_sha256": item.PackageSHA256, "permissions": item.Permissions, "download_url": "/admin/blobs/" + item.PackageSHA256,
			"pending_version_id": item.PendingVersionID, "description": item.Description,
			"updated_at": item.UpdatedAt, "created_at": item.CreatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "plugins_failed")
}

func (a *Handler) HandleAdminAPIPluginMetadataJSON(w http.ResponseWriter, r *http.Request) {
	var input store.AdminPluginMetadataRevisionInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	input.CreatedBy = a.actor(r).UserID
	version, err := a.store.AdminCreatePluginMetadataRevision(r.Context(), r.PathValue("plugin"), input)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("plugin_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

func (a *Handler) HandleAdminAPIPluginStateJSON(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if _, err := a.store.SetPluginState(r.Context(), r.PathValue("plugin"), body.State, body.Reason); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("plugin_failed", err.Error()))
		return
	}
	a.audit(r, "plugin.state", "success", r.PathValue("plugin")+" "+body.State)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIPluginGet(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminPluginV2(r.Context(), r.PathValue("plugin"))
	if err != nil {
		if errors.Is(err, store.ErrPluginNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "plugin was not found"))
		} else {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("plugin_failed", err.Error()))
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *Handler) HandleAdminAPIPluginReview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpectedVersion string `json:"expected_pending_version_id"`
		Decision        string `json:"decision"`
		Note            string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if body.ExpectedVersion == "" {
		httpx.WriteJSON(w, 428, httpx.ErrorBody("version_required", "expected_pending_version_id is required"))
		return
	}
	decision := strings.TrimSpace(body.Decision)
	note := strings.TrimSpace(body.Note)
	var state string
	switch decision {
	case "approve":
		state = "listed"
	case "reject":
		if note == "" {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "退回插件必须填写理由"))
			return
		}
		state = "rejected"
	default:
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "decision must be approve or reject"))
		return
	}
	detail, err := a.store.AdminPluginV2(r.Context(), r.PathValue("plugin"))
	if errors.Is(err, store.ErrPluginNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "plugin was not found"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("plugin_failed", err.Error()))
		return
	}
	if detail.Plugin.PendingVersionID == "" {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("plugin_failed", "plugin is not pending review"))
		return
	}
	actor := a.actor(r)
	if _, err := a.store.ReviewPluginVersion(r.Context(), r.PathValue("plugin"), state, note, body.ExpectedVersion); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("plugin_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "success", a.clientIP(r), r.UserAgent(), "plugin="+r.PathValue("plugin")+" decision="+decision)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
