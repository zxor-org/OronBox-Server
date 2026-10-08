package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

const (
	maxPluginPackageBytes = 32 << 20 // 32MB
	pluginRepoOwner       = "OronBoxCommunity"
	pluginRepoName        = "OronBox-Plugin-Repo"
	pluginRepoBranch      = "main"
)

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

func pendingPluginDir() string {
	dir := os.Getenv("PLUGIN_PENDING_DIR")
	if dir == "" {
		dir = "data/plugins/pending"
	}
	return dir
}

func pendingPluginPath(id string) string {
	return filepath.Join(pendingPluginDir(), id+".obp")
}

func savePendingPlugin(id string, data []byte) error {
	dir := pendingPluginDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(pendingPluginPath(id), data, 0644)
}

func readPendingPlugin(id string) ([]byte, error) {
	return os.ReadFile(pendingPluginPath(id))
}

func deletePendingPlugin(id string) {
	_ = os.Remove(pendingPluginPath(id))
}

func parsePluginPackage(raw []byte) (pluginManifest, map[string][]byte, error) {
	var manifest pluginManifest
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return manifest, nil, fmt.Errorf("invalid plugin package: %w", err)
	}

	files := make(map[string][]byte)
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
		content, err := readPluginEntry(file, maxPluginPackageBytes)
		if err != nil {
			return manifest, nil, err
		}
		files[name] = content
	}

	manifestBytes, ok := files["manifest.json"]
	if !ok {
		return manifest, nil, errors.New("manifest.json is missing")
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
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
	entryBytes, ok := files[entry]
	if !ok {
		return manifest, nil, fmt.Errorf("plugin entry is missing: %s", entry)
	}

	suffix := entry[strings.LastIndex(entry, ".")+1:]
	switch manifest.Runtime {
	case "wasm":
		if suffix != "wasm" {
			return manifest, nil, errors.New("wasm plugin entry must be a .wasm file")
		}
		if len(entryBytes) < 4 || entryBytes[0] != 0x00 || entryBytes[1] != 0x61 || entryBytes[2] != 0x73 || entryBytes[3] != 0x6d {
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

	if name := strings.TrimSpace(manifest.Icon); name != "" {
		name, err = normalizePluginPath(name)
		if err != nil {
			return manifest, nil, err
		}
		if _, ok := files[name]; !ok {
			return manifest, nil, fmt.Errorf("plugin icon is missing: %s", name)
		}
	}

	return manifest, files, nil
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

// pluginUpload accepts an .obp package upload, parses and validates it,
// verifies ownership, stores it into pending storage, and records/resets state='pending'.
func (a *application) pluginUpload(w http.ResponseWriter, r *http.Request) {
	u, ok := userOf(r)
	if !ok || u.ID == "" {
		jsonError(w, http.StatusUnauthorized, "unauthorized", "login required", nil)
		return
	}

	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPluginPackageBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maxPluginPackageBytes {
		jsonError(w, http.StatusBadRequest, "plugin_invalid", "invalid plugin package size", nil)
		return
	}

	manifest, _, err := parsePluginPackage(raw)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "plugin_invalid", err.Error(), nil)
		return
	}

	// Verify ID ownership if plugin already exists
	var existingOwner string
	err = a.db.Pool.QueryRow(r.Context(), `SELECT uploader_id FROM plugins WHERE id=$1`, manifest.ID).Scan(&existingOwner)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}
	if err == nil && existingOwner != u.ID {
		jsonError(w, http.StatusForbidden, "plugin_not_owned", "plugin id is owned by another user", nil)
		return
	}

	if err := savePendingPlugin(manifest.ID, raw); err != nil {
		jsonError(w, http.StatusInternalServerError, "storage_failed", "unable to save pending package: "+err.Error(), nil)
		return
	}

	pkgSHA := fmt.Sprintf("%x", sha256.Sum256(raw))
	permsJSON, _ := json.Marshal(manifest.Permissions)

	query := `
INSERT INTO plugins(id, uploader_id, name, version, author, description, runtime, permissions, state, moderation_reason, package_sha256, created_at, updated_at)
VALUES($1, $2, $3, $4, $5, $6, $7, $8, 'pending', '', $9, now(), now())
ON CONFLICT (id) DO UPDATE SET
  name=EXCLUDED.name,
  version=EXCLUDED.version,
  author=EXCLUDED.author,
  description=EXCLUDED.description,
  runtime=EXCLUDED.runtime,
  permissions=EXCLUDED.permissions,
  state='pending',
  moderation_reason='',
  package_sha256=EXCLUDED.package_sha256,
  updated_at=now()`

	if _, err := a.db.Pool.Exec(r.Context(), query, manifest.ID, u.ID, manifest.Name, manifest.Version, manifest.Author, manifest.Description, manifest.Runtime, permsJSON, pkgSHA); err != nil {
		deletePendingPlugin(manifest.ID)
		jsonError(w, http.StatusInternalServerError, "database_error", "failed to record plugin submission: "+err.Error(), nil)
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{
		"id":          manifest.ID,
		"name":        manifest.Name,
		"version":     manifest.Version,
		"author":      manifest.Author,
		"description": manifest.Description,
		"runtime":     manifest.Runtime,
		"permissions": manifest.Permissions,
		"state":       "pending",
		"sha256":      pkgSHA,
	})
}

// pluginMy lists all plugins owned by the current authenticated user along with their moderation state.
func (a *application) pluginMy(w http.ResponseWriter, r *http.Request) {
	u, ok := userOf(r)
	if !ok || u.ID == "" {
		jsonError(w, http.StatusUnauthorized, "unauthorized", "login required", nil)
		return
	}

	rows, err := a.db.Pool.Query(r.Context(), `
SELECT id, name, version, author, description, runtime, permissions, state, moderation_reason, package_sha256, created_at, updated_at
FROM plugins WHERE uploader_id=$1 ORDER BY updated_at DESC`, u.ID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, version, author, desc, runtime, state, reason, sha string
		var permsRaw []byte
		var created, updated time.Time
		if err := rows.Scan(&id, &name, &version, &author, &desc, &runtime, &permsRaw, &state, &reason, &sha, &created, &updated); err == nil {
			var perms []string
			_ = json.Unmarshal(permsRaw, &perms)
			items = append(items, map[string]any{
				"id":               id,
				"name":             name,
				"version":          version,
				"author":           author,
				"description":      desc,
				"runtime":          runtime,
				"permissions":      perms,
				"state":            state,
				"moderationReason": reason,
				"sha256":           sha,
				"createdAt":        created,
				"updatedAt":        updated,
				"owned":            true,
			})
		}
	}

	jsonResponse(w, http.StatusOK, map[string]any{"plugins": items})
}

// pluginDelete allows the author to delist/remove their plugin.
func (a *application) pluginDelete(w http.ResponseWriter, r *http.Request) {
	u, ok := userOf(r)
	if !ok || u.ID == "" {
		jsonError(w, http.StatusUnauthorized, "unauthorized", "login required", nil)
		return
	}

	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) == 0 {
		jsonError(w, http.StatusBadRequest, "id_required", "plugin id is required", nil)
		return
	}
	pluginID := parts[len(parts)-1]

	var ownerID string
	err := a.db.Pool.QueryRow(r.Context(), `SELECT uploader_id FROM plugins WHERE id=$1`, pluginID).Scan(&ownerID)
	if errors.Is(err, pgx.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "plugin_not_found", "plugin was not found", nil)
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}
	if ownerID != u.ID && u.Role != "admin" {
		jsonError(w, http.StatusForbidden, "forbidden", "cannot delete another user's plugin", nil)
		return
	}

	if _, err := a.db.Pool.Exec(r.Context(), `UPDATE plugins SET state='delisted', moderation_reason='Author removed', updated_at=now() WHERE id=$1`, pluginID); err != nil {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}

	deletePendingPlugin(pluginID)
	_ = a.syncPluginDelistToGit(r.Context(), pluginID)

	jsonResponse(w, http.StatusOK, map[string]any{"removed": true})
}

// adminPlugins lists all plugins for administrative review and management.
func (a *application) adminPlugins(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Pool.Query(r.Context(), `
SELECT p.id, p.uploader_id, COALESCE(u.username,''), p.name, p.version, p.author, p.description, p.runtime, p.permissions, p.state, p.moderation_reason, p.package_sha256, p.created_at, p.updated_at
FROM plugins p LEFT JOIN users u ON u.id=p.uploader_id ORDER BY p.updated_at DESC`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, uploaderID, username, name, version, author, desc, runtime, state, reason, sha string
		var permsRaw []byte
		var created, updated time.Time
		if err := rows.Scan(&id, &uploaderID, &username, &name, &version, &author, &desc, &runtime, &permsRaw, &state, &reason, &sha, &created, &updated); err == nil {
			var perms []string
			_ = json.Unmarshal(permsRaw, &perms)
			items = append(items, map[string]any{
				"id":               id,
				"uploader_id":      uploaderID,
				"username":         username,
				"name":             name,
				"version":          version,
				"author":           author,
				"description":      desc,
				"runtime":          runtime,
				"permissions":      perms,
				"state":            state,
				"moderation_reason": reason,
				"sha256":           sha,
				"download_url":     fmt.Sprintf("/admin/api/plugins/%s/download", id),
				"created_at":       created,
				"updated_at":       updated,
			})
		}
	}

	jsonResponse(w, http.StatusOK, map[string]any{"items": items})
}

// adminPluginDownload serves the .obp plugin package for inspection/download by administrators.
func (a *application) adminPluginDownload(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var pluginID string
	for i, part := range parts {
		if part == "plugins" && i+1 < len(parts) {
			pluginID = parts[i+1]
			break
		}
	}
	if pluginID == "" {
		jsonError(w, http.StatusBadRequest, "id_required", "plugin id is required", nil)
		return
	}

	// First try pending plugin file
	if data, err := readPendingPlugin(pluginID); err == nil {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.obp"`, pluginID))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	// Otherwise if listed in Gitea, download from Gitea or redirect
	owner := pluginRepoOwner
	repo := pluginRepoName
	branch := pluginRepoBranch
	if a.cfg.Gitea.APIURL != "" {
		redirectURL := fmt.Sprintf("%s/%s/%s/raw/branch/%s/%s/%s.obp",
			strings.TrimRight(a.cfg.Gitea.APIURL, "/api/v1"), owner, repo, branch, pluginID, pluginID)
		http.Redirect(w, r, redirectURL, http.StatusFound)
		return
	}

	jsonError(w, http.StatusNotFound, "plugin_file_not_found", "plugin package file not found", nil)
}

// adminPluginReview approves or rejects a pending plugin submission.
func (a *application) adminPluginReview(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// e.g. /admin/api/plugins/{id}/review -> parts: ["admin", "api", "plugins", "{id}", "review"]
	var pluginID string
	for i, part := range parts {
		if part == "plugins" && i+1 < len(parts) {
			pluginID = parts[i+1]
			break
		}
	}
	if pluginID == "" {
		jsonError(w, http.StatusBadRequest, "id_required", "plugin id is required", nil)
		return
	}

	var in struct {
		Action string `json:"action"` // "approve" or "reject"
		Note   string `json:"note"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Note == "" {
		in.Note = in.Reason
	}

	var uploaderID, name, version, state string
	err := a.db.Pool.QueryRow(r.Context(), `SELECT uploader_id, name, version, state FROM plugins WHERE id=$1`, pluginID).Scan(&uploaderID, &name, &version, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "plugin_not_found", "plugin not found", nil)
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}

	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "approve":
		rawPkg, err := readPendingPlugin(pluginID)
		if err != nil {
			// If already listed or no pending package on disk, verify if we can just update state
			if state != "listed" {
				jsonError(w, http.StatusBadRequest, "pending_package_missing", "cannot find pending package file on disk to publish", nil)
				return
			}
		} else {
			if err := a.publishPluginToGit(r.Context(), pluginID, rawPkg); err != nil {
				jsonError(w, http.StatusInternalServerError, "git_sync_failed", "failed to publish plugin to Git: "+err.Error(), nil)
				return
			}
			deletePendingPlugin(pluginID)
		}

		if _, err := a.db.Pool.Exec(r.Context(), `UPDATE plugins SET state='listed', moderation_reason='', updated_at=now() WHERE id=$1`, pluginID); err != nil {
			jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
			return
		}

		_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO user_messages(user_id, kind, event, title, body, ref) VALUES($1, 'plugin', 'plugin.approved', $2, $3, $4)`,
			uploaderID, "插件审核通过", fmt.Sprintf("您的插件 %s (v%s) 已通过审核并上架", name, version), pluginID)

		jsonResponse(w, http.StatusOK, map[string]any{"status": "listed"})

	case "reject":
		deletePendingPlugin(pluginID)
		if _, err := a.db.Pool.Exec(r.Context(), `UPDATE plugins SET state='rejected', moderation_reason=$2, updated_at=now() WHERE id=$1`, pluginID, in.Note); err != nil {
			jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
			return
		}

		_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO user_messages(user_id, kind, event, title, body, ref) VALUES($1, 'plugin', 'plugin.rejected', $2, $3, $4)`,
			uploaderID, "插件审核未通过", fmt.Sprintf("您的插件 %s (v%s) 审核未通过，原因：%s", name, version, in.Note), pluginID)

		jsonResponse(w, http.StatusOK, map[string]any{"status": "rejected"})

	default:
		jsonError(w, http.StatusBadRequest, "invalid_action", "action must be 'approve' or 'reject'", nil)
	}
}

// adminPluginState updates listed/delisted status of a plugin.
func (a *application) adminPluginState(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	var pluginID string
	for i, part := range parts {
		if part == "plugins" && i+1 < len(parts) {
			pluginID = parts[i+1]
			break
		}
	}
	if pluginID == "" {
		jsonError(w, http.StatusBadRequest, "id_required", "plugin id is required", nil)
		return
	}

	var in struct {
		Action string `json:"action"` // "delist" or "relist"
		Note   string `json:"note"`
		Reason string `json:"reason"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Note == "" {
		in.Note = in.Reason
	}

	var uploaderID, name, version string
	err := a.db.Pool.QueryRow(r.Context(), `SELECT uploader_id, name, version FROM plugins WHERE id=$1`, pluginID).Scan(&uploaderID, &name, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		jsonError(w, http.StatusNotFound, "plugin_not_found", "plugin not found", nil)
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}

	switch strings.ToLower(strings.TrimSpace(in.Action)) {
	case "delist":
		if _, err := a.db.Pool.Exec(r.Context(), `UPDATE plugins SET state='delisted', moderation_reason=$2, updated_at=now() WHERE id=$1`, pluginID, in.Note); err != nil {
			jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
			return
		}
		_ = a.syncPluginDelistToGit(r.Context(), pluginID)

		_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO user_messages(user_id, kind, event, title, body, ref) VALUES($1, 'plugin', 'plugin.delisted', $2, $3, $4)`,
			uploaderID, "插件已下架", fmt.Sprintf("您的插件 %s (v%s) 已被管理员下架，原因：%s", name, version, in.Note), pluginID)

		jsonResponse(w, http.StatusOK, map[string]any{"status": "delisted"})

	case "relist":
		if _, err := a.db.Pool.Exec(r.Context(), `UPDATE plugins SET state='listed', moderation_reason='', updated_at=now() WHERE id=$1`, pluginID); err != nil {
			jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
			return
		}
		_ = a.syncPluginRelistToGit(r.Context(), pluginID)

		_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO user_messages(user_id, kind, event, title, body, ref) VALUES($1, 'plugin', 'plugin.relisted', $2, $3, $4)`,
			uploaderID, "插件已恢复上架", fmt.Sprintf("您的插件 %s (v%s) 已恢复上架", name, version), pluginID)

		jsonResponse(w, http.StatusOK, map[string]any{"status": "listed"})

	default:
		jsonError(w, http.StatusBadRequest, "invalid_action", "action must be 'delist' or 'relist'", nil)
	}
}

// publishPluginToGit unzips the .obp package and commits all files into OronBox-Plugin-Repo under <id>/,
// and ensures <id> is listed in index.txt.
func (a *application) publishPluginToGit(ctx context.Context, pluginID string, rawPackage []byte) error {
	if a.cfg.Gitea.ClientSecret == "" {
		return nil // Skip git publication in environments without Gitea bot credentials
	}

	manifest, files, err := parsePluginPackage(rawPackage)
	if err != nil {
		return err
	}

	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}

	parentCommit, err := client.GetBranchCommit(ctx, pluginRepoOwner, pluginRepoName, pluginRepoBranch)
	if err != nil {
		return fmt.Errorf("read plugin repo branch %s: %w", pluginRepoBranch, err)
	}
	treeSHA, err := client.CommitTree(ctx, pluginRepoOwner, pluginRepoName, parentCommit)
	if err != nil {
		return fmt.Errorf("read plugin repo commit tree: %w", err)
	}

	indexBytes, err := client.ReadFile(ctx, pluginRepoOwner, pluginRepoName, "index.txt", pluginRepoBranch)
	if err != nil && !strings.Contains(err.Error(), "404") {
		return fmt.Errorf("read index.txt: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(indexBytes), "\r\n", "\n"), "\n")
	hasID := false
	cleanedLines := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if trimmed == pluginID {
			hasID = true
		}
		cleanedLines = append(cleanedLines, trimmed)
	}
	if !hasID {
		cleanedLines = append(cleanedLines, pluginID)
	}
	newIndex := strings.Join(cleanedLines, "\n") + "\n"

	commitFiles := make(map[string][]byte, len(files)+1)
	commitFiles["index.txt"] = []byte(newIndex)
	for filePath, fileBytes := range files {
		commitFiles[pluginID+"/"+filePath] = fileBytes
	}

	msg := fmt.Sprintf("Publish plugin %s v%s", manifest.Name, manifest.Version)
	_, err = client.CommitFilesTreeBase(ctx, pluginRepoOwner, pluginRepoName, pluginRepoBranch, msg, treeSHA, commitFiles)
	return err
}

// syncPluginDelistToGit removes the plugin ID from index.txt in OronBox-Plugin-Repo.
func (a *application) syncPluginDelistToGit(ctx context.Context, pluginID string) error {
	if a.cfg.Gitea.ClientSecret == "" {
		return nil
	}

	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	indexBytes, err := client.ReadFile(ctx, pluginRepoOwner, pluginRepoName, "index.txt", pluginRepoBranch)
	if err != nil {
		return err
	}

	lines := strings.Split(strings.ReplaceAll(string(indexBytes), "\r\n", "\n"), "\n")
	cleanedLines := make([]string, 0, len(lines))
	changed := false
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if trimmed == pluginID {
			changed = true
			continue
		}
		cleanedLines = append(cleanedLines, trimmed)
	}

	if !changed {
		return nil
	}

	newIndex := strings.Join(cleanedLines, "\n") + "\n"
	return client.UpsertFile(ctx, pluginRepoOwner, pluginRepoName, "index.txt", pluginRepoBranch, fmt.Sprintf("Delist plugin %s", pluginID), []byte(newIndex))
}

// syncPluginRelistToGit appends the plugin ID to index.txt in OronBox-Plugin-Repo if missing.
func (a *application) syncPluginRelistToGit(ctx context.Context, pluginID string) error {
	if a.cfg.Gitea.ClientSecret == "" {
		return nil
	}

	client := syndication.GiteaClient{BaseURL: a.cfg.Gitea.APIURL, Token: a.cfg.Gitea.ClientSecret}
	indexBytes, err := client.ReadFile(ctx, pluginRepoOwner, pluginRepoName, "index.txt", pluginRepoBranch)
	if err != nil && !strings.Contains(err.Error(), "404") {
		return err
	}

	lines := strings.Split(strings.ReplaceAll(string(indexBytes), "\r\n", "\n"), "\n")
	hasID := false
	cleanedLines := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if trimmed == pluginID {
			hasID = true
		}
		cleanedLines = append(cleanedLines, trimmed)
	}

	if hasID {
		return nil
	}

	cleanedLines = append(cleanedLines, pluginID)
	newIndex := strings.Join(cleanedLines, "\n") + "\n"
	return client.UpsertFile(ctx, pluginRepoOwner, pluginRepoName, "index.txt", pluginRepoBranch, fmt.Sprintf("Relist plugin %s", pluginID), []byte(newIndex))
}

// pluginListPublic serves the public catalog of listed plugins (GET /api/v1/plugins)
func (a *application) pluginListPublic(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		jsonResponse(w, http.StatusOK, map[string]any{"plugins": []any{}})
		return
	}
	viewerID := ""
	if u, ok := userOf(r); ok {
		viewerID = u.ID
	}

	rows, err := a.db.Pool.Query(r.Context(), `
SELECT p.id, p.name, p.version, p.author, p.description, p.runtime, p.permissions, p.state, p.moderation_reason,
       p.uploader_id, COALESCE(u.username, '')
FROM plugins p
LEFT JOIN users u ON u.id = p.uploader_id
WHERE p.state = 'listed'
ORDER BY p.updated_at DESC`)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "database_error", err.Error(), nil)
		return
	}
	defer rows.Close()

	items := make([]map[string]any, 0)
	for rows.Next() {
		var (
			id, name, version, author, desc, runtime, modReason, uploaderID, username string
			permsRaw                                                                  []byte
			state                                                                     string
		)
		if err := rows.Scan(&id, &name, &version, &author, &desc, &runtime, &permsRaw, &state, &modReason, &uploaderID, &username); err != nil {
			continue
		}
		var perms []string
		_ = json.Unmarshal(permsRaw, &perms)
		if perms == nil {
			perms = []string{}
		}

		packageURL := fmt.Sprintf("/plugins/%s/%s.obp", id, id)
		iconURL := fmt.Sprintf("/plugins/%s/icon.png", id)

		item := map[string]any{
			"id":               id,
			"name":             name,
			"version":          version,
			"author":           author,
			"description":      desc,
			"runtime":          runtime,
			"permissions":      perms,
			"state":            state,
			"moderationReason": modReason,
			"packageUrl":       packageURL,
			"iconUrl":          iconURL,
			"uploader": map[string]any{
				"id":       uploaderID,
				"username": username,
			},
			"owned": viewerID != "" && viewerID == uploaderID,
		}
		items = append(items, item)
	}

	jsonResponse(w, http.StatusOK, map[string]any{"plugins": items})
}
