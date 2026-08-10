package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

const maxPluginPackageBytes = 32 << 20

var (
	pluginIDPattern       = regexp.MustCompile(`^[a-z][a-z0-9]*([.-][a-z0-9][a-z0-9-]*)+$`)
	pluginPermissionNames = map[string]bool{
		"ui": true, "file": true, "network": true, "interconnect": true,
		"provider": true, "device": true, "protocol": true, "appside": true,
	}
)

type pluginManifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	APILevel    int      `json:"api_level"`
	Runtime     string   `json:"runtime"`
	Entry       string   `json:"entry"`
	Icon        string   `json:"icon"`
	Permissions []string `json:"permissions"`
}

func pluginJSON(plugin store.PluginRecord, viewerID string) map[string]any {
	value := map[string]any{
		"id":          plugin.ID,
		"name":        plugin.Name,
		"version":     plugin.Version,
		"author":      plugin.Author,
		"description": plugin.Description,
		"runtime":     plugin.Runtime,
		"permissions": plugin.Permissions,
		"size":        plugin.PackageSize,
		"sha256":      plugin.PackageSHA256,
		"packageUrl":  "/api/plugins/" + plugin.ID + "/package",
		"uploader":    map[string]string{"id": plugin.UploaderID, "username": plugin.UploaderName},
		"updatedAt":   plugin.UpdatedAt.Format(time.RFC3339),
	}
	if plugin.Permissions == nil {
		value["permissions"] = []string{}
	}
	if plugin.IconSHA256 != "" {
		value["iconUrl"] = "/api/plugins/" + plugin.ID + "/icon"
	}
	if viewerID != "" && plugin.UploaderID == viewerID {
		value["owned"] = true
		value["state"] = plugin.State
		if plugin.PendingVersionID != "" {
			value["state"] = plugin.PendingState
			value["pendingVersionId"] = plugin.PendingVersionID
		}
		if plugin.ModerationReason != "" {
			value["moderationReason"] = plugin.ModerationReason
		}
		if plugin.PendingReason != "" {
			value["moderationReason"] = plugin.PendingReason
		}
	}
	return value
}

// optionalUserID extracts the caller's user id when a valid Bearer token is
// present; the plugin catalog stays public either way.
func (a *App) optionalUserID(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
		return ""
	}
	user, err := a.store.UserByAccessToken(r.Context(), authcore.HashToken(strings.TrimSpace(header[7:]), a.cfg.SessionSecret))
	if err != nil {
		return ""
	}
	return user.ID
}

func (a *App) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	viewerID := a.optionalUserID(r)
	plugins, err := a.store.ListPlugins(r.Context(), viewerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("plugins_failed", err.Error()))
		return
	}
	items := make([]any, 0, len(plugins))
	for _, plugin := range plugins {
		items = append(items, pluginJSON(plugin, viewerID))
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": items})
}

// handleUploadPlugin accepts a raw .obp body, validates the embedded manifest
// itself (client-supplied metadata is never trusted) and publishes the
// package. Re-uploading an id replaces it while it belongs to the caller.
func (a *App) handleUploadPlugin(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPluginPackageBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxPluginPackageBytes {
		writeJSON(w, http.StatusBadRequest, errorBody("plugin_invalid", "invalid plugin package size"))
		return
	}
	manifest, icon, err := parsePluginPackage(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("plugin_invalid", err.Error()))
		return
	}
	ctx := r.Context()
	packageObject, err := a.blobs.Put(ctx, bytes.NewReader(raw))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("blob_failed", err.Error()))
		return
	}
	iconSHA := ""
	if len(icon) > 0 {
		iconObject, err := a.blobs.Put(ctx, bytes.NewReader(icon))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("blob_failed", err.Error()))
			return
		}
		iconSHA = iconObject.SHA256
		if err := a.store.EnsureBlob(ctx, iconSHA, iconObject.Size, http.DetectContentType(icon), iconObject.Key); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("blob_failed", err.Error()))
			return
		}
	}
	if err := a.store.EnsureBlob(ctx, packageObject.SHA256, packageObject.Size, "application/zip", packageObject.Key); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("blob_failed", err.Error()))
		return
	}
	record := store.PluginRecord{
		ID:            manifest.ID,
		UploaderID:    user.ID,
		Name:          manifest.Name,
		Version:       manifest.Version,
		Author:        manifest.Author,
		Description:   manifest.Description,
		Runtime:       manifest.Runtime,
		Permissions:   manifest.Permissions,
		PackageSHA256: packageObject.SHA256,
		IconSHA256:    iconSHA,
	}
	owned, err := a.store.UpsertPlugin(ctx, record)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("plugin_save_failed", err.Error()))
		return
	}
	if !owned {
		writeJSON(w, http.StatusForbidden, errorBody("plugin_not_owned", "plugin id is published by another user"))
		return
	}
	plugin, err := a.store.Plugin(ctx, manifest.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("plugin_save_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, pluginJSON(plugin, user.ID))
}

func (a *App) handleDeletePlugin(w http.ResponseWriter, r *http.Request) {
	removed, err := a.store.DeletePlugin(r.Context(), r.PathValue("id"), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("plugin_delete_failed", err.Error()))
		return
	}
	if !removed {
		writeJSON(w, http.StatusNotFound, errorBody("plugin_not_found", "plugin was not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": true})
}

func (a *App) handlePluginPackage(w http.ResponseWriter, r *http.Request) {
	if !a.downloadLimiter.allow(a.clientIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, errorBody("rate_limited", "too many downloads, slow down"))
		return
	}
	plugin, err := a.store.Plugin(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrPluginNotFound) || plugin.State != "listed" {
		writeJSON(w, http.StatusNotFound, errorBody("plugin_not_found", "plugin was not found"))
		return
	}
	name := fmt.Sprintf("%s-%s.obp", plugin.ID, plugin.Version)
	a.servePluginBlob(w, r, plugin.PackageSHA256, "application/zip", name)
}

func (a *App) handlePluginIcon(w http.ResponseWriter, r *http.Request) {
	plugin, err := a.store.Plugin(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrPluginNotFound) || plugin.IconSHA256 == "" || plugin.State != "listed" {
		writeJSON(w, http.StatusNotFound, errorBody("plugin_not_found", "plugin icon was not found"))
		return
	}
	blob, err := a.store.Blob(r.Context(), plugin.IconSHA256)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("plugin_not_found", "plugin icon was not found"))
		return
	}
	a.servePluginBlob(w, r, plugin.IconSHA256, blob.MediaType, "")
}

func (a *App) servePluginBlob(w http.ResponseWriter, r *http.Request, sha256, mediaType, downloadName string) {
	record, err := a.store.Blob(r.Context(), sha256)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("blob_not_found", "blob was not found"))
		return
	}
	reader, err := a.blobs.Open(r.Context(), record.LocalKey)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("blob_not_found", "local blob was not found"))
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("ETag", `"`+record.SHA256+`"`)
	if downloadName != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, record.SHA256, time.Time{}, reader)
}

// parsePluginPackage validates an .obp archive and returns its manifest plus
// the icon bytes when declared. The rules mirror the client-side reader:
// native OronBox plugins only (runtime js/wasm/hybrid), api_level 1.
func parsePluginPackage(raw []byte) (pluginManifest, []byte, error) {
	var manifest pluginManifest
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return manifest, nil, fmt.Errorf("invalid plugin package: %w", err)
	}
	files := map[string]*zip.File{}
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name, err := normalizePluginPath(file.Name)
		if err != nil {
			return manifest, nil, err
		}
		if _, duplicate := files[name]; duplicate {
			return manifest, nil, fmt.Errorf("duplicate package entry: %s", name)
		}
		files[name] = file
	}
	manifestFile, ok := files["manifest.json"]
	if !ok {
		return manifest, nil, errors.New("manifest.json is missing")
	}
	content, err := readPluginEntry(manifestFile, 1<<20)
	if err != nil {
		return manifest, nil, err
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return manifest, nil, fmt.Errorf("invalid manifest: %w", err)
	}
	if manifest.APILevel != 1 {
		return manifest, nil, fmt.Errorf("unsupported plugin API level: %d", manifest.APILevel)
	}
	switch manifest.Runtime {
	case "js", "wasm", "hybrid":
	case "":
		return manifest, nil, errors.New("legacy AstroBox plugins are not accepted")
	default:
		return manifest, nil, fmt.Errorf("unsupported plugin runtime: %s", manifest.Runtime)
	}
	manifest.ID = strings.TrimSpace(manifest.ID)
	if !pluginIDPattern.MatchString(manifest.ID) {
		return manifest, nil, fmt.Errorf("invalid plugin id: %s", manifest.ID)
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	if manifest.Name == "" {
		return manifest, nil, errors.New("plugin name is missing")
	}
	manifest.Version = strings.TrimSpace(manifest.Version)
	if manifest.Version == "" {
		return manifest, nil, errors.New("plugin version is missing")
	}
	entry := strings.TrimSpace(manifest.Entry)
	if entry == "" {
		entry = "main.js"
	}
	entry, err = normalizePluginPath(entry)
	if err != nil {
		return manifest, nil, err
	}
	entryFile, ok := files[entry]
	if !ok {
		return manifest, nil, fmt.Errorf("plugin entry is missing: %s", entry)
	}
	suffix := entry[strings.LastIndex(entry, ".")+1:]
	switch manifest.Runtime {
	case "wasm":
		if suffix != "wasm" {
			return manifest, nil, errors.New("wasm plugin entry must be a .wasm file")
		}
		header, err := readPluginEntryHeader(entryFile, 4)
		if err != nil {
			return manifest, nil, err
		}
		if len(header) < 4 || header[0] != 0x00 || header[1] != 0x61 || header[2] != 0x73 || header[3] != 0x6d {
			return manifest, nil, errors.New("wasm plugin entry has an invalid WebAssembly header")
		}
	default:
		if suffix != "js" && suffix != "mjs" && suffix != "cjs" {
			return manifest, nil, errors.New("js and hybrid plugin entries must be JavaScript")
		}
	}
	for _, permission := range manifest.Permissions {
		if !pluginPermissionNames[permission] {
			return manifest, nil, fmt.Errorf("unsupported plugin permission: %s", permission)
		}
	}
	var icon []byte
	if name := strings.TrimSpace(manifest.Icon); name != "" {
		name, err = normalizePluginPath(name)
		if err != nil {
			return manifest, nil, err
		}
		iconFile, ok := files[name]
		if !ok {
			return manifest, nil, fmt.Errorf("plugin icon is missing: %s", name)
		}
		icon, err = readPluginEntry(iconFile, 4<<20)
		if err != nil {
			return manifest, nil, err
		}
	}
	return manifest, icon, nil
}

func normalizePluginPath(value string) (string, error) {
	path := strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(path, "/") || strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("unsafe package path: %s", value)
	}
	parts := strings.Split(path, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "." {
			continue
		}
		if part == "" || part == ".." || strings.Contains(part, ":") {
			return "", fmt.Errorf("unsafe package path: %s", value)
		}
		clean = append(clean, part)
	}
	return strings.Join(clean, "/"), nil
}

func readPluginEntryHeader(file *zip.File, length int) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, int64(length)))
}

func readPluginEntry(file *zip.File, limit int64) ([]byte, error) {
	if file.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("package entry is too large: %s", file.Name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, limit))
}
