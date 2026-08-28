package server

import (
	"bytes"
	"encoding/json"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	resourcecore "github.com/zxor-org/OronBox-Server/internal/resource"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminRevisionMediaUpload(w http.ResponseWriter, r *http.Request) {
	if err := a.parseAdminUpload(w, r, a.cfg.Limits.PreviewMaxBytes); err != nil {
		a.rejectAdminUpload(w, r, err)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, a.cfg.Limits.PreviewMaxBytes+1))
	if err != nil || int64(len(payload)) > a.cfg.Limits.PreviewMaxBytes {
		http.Error(w, "image is too large", http.StatusBadRequest)
		return
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 1500 || config.Height > 1500 {
		http.Error(w, "invalid image dimensions", http.StatusBadRequest)
		return
	}
	object, err := a.blobs.Put(r.Context(), bytes.NewReader(payload))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.store.EnsureBlob(r.Context(), object.SHA256, object.Size, "image/"+format, object.Key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = a.store.AdminAddRevisionMedia(r.Context(), r.PathValue("resource"), r.PathValue("revision"), store.AdminMediaInput{SHA256: object.SHA256, Role: r.FormValue("role"), Width: config.Width, Height: config.Height})
	a.adminRevisionAssetResult(w, r, "media.upload", err)
}

func (a *App) handleAdminRevisionArtifactUpload(w http.ResponseWriter, r *http.Request) {
	if err := a.parseAdminUpload(w, r, a.cfg.Limits.UploadMaxBytes); err != nil {
		a.rejectAdminUpload(w, r, err)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "resource file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, a.cfg.Limits.UploadMaxBytes+1))
	if err != nil || int64(len(raw)) > a.cfg.Limits.UploadMaxBytes {
		http.Error(w, "resource file is too large", http.StatusBadRequest)
		return
	}
	analysis, err := resourcecore.Analyze(raw)
	if err != nil || analysis.Platform != resourcecore.VelaOS || (analysis.Kind != resourcecore.QuickApp && analysis.Kind != resourcecore.Watchface) {
		http.Error(w, "unsupported VelaOS resource file", http.StatusBadRequest)
		return
	}
	detail, err := a.store.AdminResourceRevision(r.Context(), r.PathValue("resource"), r.PathValue("revision"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if (detail.Resource.Kind == "quickapp") != (analysis.Kind == resourcecore.QuickApp) {
		http.Error(w, "resource file kind does not match resource", http.StatusBadRequest)
		return
	}
	object, err := a.blobs.Put(r.Context(), bytes.NewReader(analysis.Payload))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.store.EnsureBlob(r.Context(), object.SHA256, object.Size, "application/octet-stream", object.Key); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	analysisJSON, _ := json.Marshal(analysis)
	var analysisValue map[string]any
	_ = json.Unmarshal(analysisJSON, &analysisValue)
	err = a.store.AdminAddRevisionArtifact(r.Context(), r.PathValue("resource"), r.PathValue("revision"), store.AdminArtifactInput{SHA256: object.SHA256, OriginalName: filepath.Base(header.Filename), PackageFormat: analysis.PackageFormat, PackageID: analysis.PackageID, Version: analysis.Version, Analysis: analysisValue, DeviceIDs: r.Form["device_ids"]})
	a.adminRevisionAssetResult(w, r, "artifact.upload", err)
}

func (a *App) adminRevisionAssetResult(w http.ResponseWriter, r *http.Request, action string, err error) {
	actor := currentAdmin(r)
	detail := "resource=" + r.PathValue("resource") + " revision=" + r.PathValue("revision")
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource."+action, "failure", a.clientIP(r), r.UserAgent(), detail+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource."+action, "success", a.clientIP(r), r.UserAgent(), detail)
	if wantsJSON(r) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if reviewID := strings.TrimSpace(r.FormValue("return_review")); reviewID != "" {
		http.Redirect(w, r, "/admin/review/"+reviewID+"?action="+strings.ReplaceAll(action, ".", "_"), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/resources/"+r.PathValue("resource")+"/draft?action="+strings.ReplaceAll(action, ".", "_"), http.StatusFound)
}
