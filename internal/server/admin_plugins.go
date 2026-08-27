package server

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminPluginDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminPluginV2(r.Context(), r.PathValue("plugin"))
	if errors.Is(err, store.ErrPluginNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.render(w, r, "admin_plugin_workspace", map[string]any{"Title": detail.Plugin.Name, "Detail": detail, "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminPluginPackageRevision(w http.ResponseWriter, r *http.Request) {
	if err := a.parseAdminUpload(w, r, maxPluginPackageBytes); err != nil {
		a.rejectAdminUpload(w, r, err)
		return
	}
	file, _, err := r.FormFile("package")
	if err != nil {
		http.Error(w, "plugin package is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxPluginPackageBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxPluginPackageBytes {
		http.Error(w, "invalid plugin package size", http.StatusBadRequest)
		return
	}
	manifest, icon, err := parsePluginPackage(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if manifest.ID != r.PathValue("plugin") {
		http.Error(w, "package plugin id does not match", http.StatusConflict)
		return
	}
	object, err := a.blobs.Put(r.Context(), bytes.NewReader(raw))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err = a.store.EnsureBlob(r.Context(), object.SHA256, object.Size, "application/zip", object.Key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	iconSHA := ""
	if len(icon) > 0 {
		iconObject, putErr := a.blobs.Put(r.Context(), bytes.NewReader(icon))
		if putErr != nil {
			http.Error(w, putErr.Error(), 500)
			return
		}
		iconSHA = iconObject.SHA256
		if err = a.store.EnsureBlob(r.Context(), iconSHA, iconObject.Size, http.DetectContentType(icon), iconObject.Key); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	}
	actor := currentAdmin(r)
	version, err := a.store.AdminCreatePluginPackageRevision(r.Context(), manifest.ID, store.AdminPluginPackageRevisionInput{Version: manifest.Version, Name: manifest.Name, Author: manifest.Author, Description: manifest.Description, Runtime: manifest.Runtime, Permissions: manifest.Permissions, PackageSHA256: object.SHA256, IconSHA256: iconSHA, CreatedBy: actor.UserID})
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin.package.revision.create", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin.package.revision.create", "success", a.clientIP(r), r.UserAgent(), "plugin="+manifest.ID+" version="+version.ID)
	http.Redirect(w, r, "/admin/plugins/"+manifest.ID+"?action=package_drafted", http.StatusFound)
}
func (a *App) handleAdminPluginMetadata(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	actor := currentAdmin(r)
	version, err := a.store.AdminCreatePluginMetadataRevision(r.Context(), r.PathValue("plugin"), store.AdminPluginMetadataRevisionInput{Name: r.FormValue("name"), Author: r.FormValue("author"), Description: r.FormValue("description"), CreatedBy: actor.UserID})
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin.revision.create", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), 409)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin.revision.create", "success", a.clientIP(r), r.UserAgent(), "plugin="+r.PathValue("plugin")+" version="+version.ID)
	http.Redirect(w, r, "/admin/plugins/"+r.PathValue("plugin")+"?action=drafted", 302)
}

func (a *App) handleAdminPluginsPage(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminPluginsV2(r.Context(), store.AdminPluginQuery{Search: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Uploader: r.URL.Query().Get("uploader"), Runtime: r.URL.Query().Get("runtime"), Sort: r.URL.Query().Get("sort"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_plugins", map[string]any{"Title": "插件管理", "Items": page.Items, "Page": page, "Query": page.Query, "Pager": web.NewPagination("/admin/plugins", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminPluginReview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	pluginID := r.PathValue("plugin")
	decision := strings.TrimSpace(r.FormValue("decision"))
	note := strings.TrimSpace(r.FormValue("note"))
	var state, reason string
	switch decision {
	case "approve":
		state = "listed"
	case "reject":
		if note == "" {
			http.Error(w, "reject reason is required", http.StatusBadRequest)
			return
		}
		state, reason = "rejected", note
	default:
		http.Error(w, "unknown decision", http.StatusBadRequest)
		return
	}
	pluginDetail, err := a.store.AdminPluginV2(r.Context(), pluginID)
	if errors.Is(err, store.ErrPluginNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pluginDetail.Plugin.PendingVersionID == "" {
		http.Error(w, "plugin is not pending review", http.StatusConflict)
		return
	}
	if _, err := a.store.SetPluginState(r.Context(), pluginID, state, reason); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "failure", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "success", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" decision="+decision+" note="+note)
	observability.From(r.Context()).With("component", "admin").Info(
		"admin plugin reviewed",
		"plugin_id", pluginID,
		"decision", decision,
		"admin_user", actor.Username,
		"reason", note,
	)
	http.Redirect(w, r, "/admin/plugins?action=reviewed", http.StatusFound)
}

func (a *App) handleAdminPluginState(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	pluginID := r.PathValue("plugin")
	action := strings.TrimSpace(r.FormValue("action"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	var state, from string
	switch action {
	case "delist":
		if reason == "" {
			http.Error(w, "delist reason is required", http.StatusBadRequest)
			return
		}
		state, from = "delisted", "listed"
	case "restore":
		state, from, reason = "listed", "delisted", ""
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	plugin, err := a.store.Plugin(r.Context(), pluginID)
	if errors.Is(err, store.ErrPluginNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if plugin.State != from {
		http.Error(w, "plugin state does not allow this action", http.StatusConflict)
		return
	}
	if _, err := a.store.SetPluginState(r.Context(), pluginID, state, reason); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin."+action, "failure", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin."+action, "success", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" reason="+reason)
	observability.From(r.Context()).With("component", "admin").Info(
		"admin plugin state changed",
		"plugin_id", pluginID,
		"action", action,
		"admin_user", actor.Username,
		"reason", reason,
	)
	http.Redirect(w, r, "/admin/plugins?action="+action, http.StatusFound)
}
