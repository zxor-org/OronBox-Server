package server

import (
	"bytes"
	"errors"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"io"
	"net/http"
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
	a.render(w, "admin_plugin_workspace", map[string]any{"Title": detail.Plugin.Name, "Detail": detail, "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminPluginPackageRevision(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPluginPackageBytes+(1<<20))
	if err := r.ParseMultipartForm(maxPluginPackageBytes + 1); err != nil {
		http.Error(w, "invalid package upload", http.StatusBadRequest)
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
