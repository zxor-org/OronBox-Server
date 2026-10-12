package catalog

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

const (
	maxPluginPackageBytes = 32 << 20
	maxPluginEntryBytes   = 8 << 20
	maxPluginExpanded     = 64 << 20
	maxPluginEntries      = 4096
)

var supportedPluginPermissions = map[string]struct{}{
	"ui": {}, "file": {}, "network": {}, "interconnect": {},
	"provider": {}, "device": {}, "protocol": {}, "appside": {},
}

type pluginManifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Runtime     string   `json:"runtime"`
	Entry       string   `json:"entry"`
	Icon        string   `json:"icon"`
	Permissions []string `json:"permissions"`
	APILevel    int      `json:"api_level"`
}

func validatePluginPackage(data []byte) (pluginManifest, error) {
	var meta pluginManifest
	if len(data) == 0 || len(data) > maxPluginPackageBytes {
		return meta, fmt.Errorf("插件包为空或超过 32 MiB")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) == 0 || len(zr.File) > maxPluginEntries {
		return meta, fmt.Errorf("插件包不是有效的 ZIP")
	}
	seen := make(map[string]bool, len(zr.File))
	var manifest []byte
	var expanded uint64
	for _, entry := range zr.File {
		rawName := strings.ReplaceAll(entry.Name, "\\", "/")
		directory := strings.HasSuffix(rawName, "/")
		parts := strings.Split(strings.TrimSuffix(rawName, "/"), "/")
		if rawName == "" || strings.IndexByte(rawName, 0) >= 0 || strings.HasPrefix(rawName, "/") ||
			entry.Mode()&0o170000 == 0o120000 {
			return meta, fmt.Errorf("插件包路径无效")
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." {
				return meta, fmt.Errorf("插件包路径无效")
			}
		}
		name := strings.Join(parts, "/")
		if seen[name] {
			return meta, fmt.Errorf("插件包包含重复文件")
		}
		seen[name] = true
		if directory {
			continue
		}
		if entry.UncompressedSize64 > maxPluginEntryBytes {
			return meta, fmt.Errorf("插件包单文件超过 8 MiB")
		}
		expanded += entry.UncompressedSize64
		if expanded > maxPluginExpanded {
			return meta, fmt.Errorf("插件包解压后超过限制")
		}
		if name == "manifest.json" {
			f, openErr := entry.Open()
			if openErr != nil {
				return meta, fmt.Errorf("manifest 无法读取")
			}
			manifest, err = io.ReadAll(io.LimitReader(f, 1<<20))
			_ = f.Close()
			if err != nil {
				return meta, fmt.Errorf("manifest 无法读取")
			}
		}
	}
	if len(manifest) == 0 || json.Unmarshal(manifest, &meta) != nil {
		return meta, fmt.Errorf("缺少或无法解析 manifest.json")
	}
	meta.ID = strings.TrimSpace(meta.ID)
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Version = strings.TrimSpace(meta.Version)
	meta.Runtime = strings.TrimSpace(meta.Runtime)
	meta.Entry = strings.TrimSpace(strings.ReplaceAll(meta.Entry, "\\", "/"))
	meta.Icon = strings.TrimSpace(strings.ReplaceAll(meta.Icon, "\\", "/"))
	if meta.ID == "" || !regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9][a-z0-9-]*)+$`).MatchString(meta.ID) ||
		meta.Name == "" || meta.Version == "" || len(meta.Name) > 128 || len(meta.Version) > 64 || meta.APILevel != 1 {
		return meta, fmt.Errorf("插件 manifest 字段无效")
	}
	if meta.Runtime == "" {
		return meta, fmt.Errorf("插件 manifest 缺少 runtime")
	}
	if meta.Runtime != "js" && meta.Runtime != "wasm" && meta.Runtime != "hybrid" {
		return meta, fmt.Errorf("插件运行时无效")
	}
	if meta.Entry == "" {
		meta.Entry = "main.js"
	}
	if strings.HasPrefix(meta.Entry, "/") || strings.Contains(meta.Entry, "../") || seen[meta.Entry] == false {
		return meta, fmt.Errorf("插件入口文件不存在")
	}
	suffix := strings.ToLower(path.Ext(meta.Entry))
	if meta.Runtime == "wasm" && suffix != ".wasm" || (meta.Runtime == "js" || meta.Runtime == "hybrid") && suffix != ".js" && suffix != ".mjs" && suffix != ".cjs" {
		return meta, fmt.Errorf("插件入口文件类型与运行时不匹配")
	}
	if meta.Icon != "" && !seen[meta.Icon] {
		return meta, fmt.Errorf("插件图标文件不存在")
	}
	permSeen := map[string]bool{}
	for _, permission := range meta.Permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" || permSeen[permission] {
			return meta, fmt.Errorf("插件权限无效")
		}
		if _, ok := supportedPluginPermissions[permission]; !ok {
			return meta, fmt.Errorf("插件权限不受支持: %s", permission)
		}
		permSeen[permission] = true
	}
	return meta, nil
}

func samePluginPermissions(raw json.RawMessage, permissions []string) bool {
	var current []string
	if json.Unmarshal(raw, &current) != nil || len(current) != len(permissions) {
		return false
	}
	set := make(map[string]struct{}, len(current))
	for _, value := range current {
		set[value] = struct{}{}
	}
	for _, value := range permissions {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}

// HandleUploadPlugin accepts an ABP zip, stores it as a content-addressed blob
// and creates a pending plugin version. The package is never made visible
// until an administrator approves that version.
func HandleUploadPlugin(s *store.Store, storageDir string, r2 *R2Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxPluginPackageBytes+1)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			httputil.Error(w, http.StatusRequestEntityTooLarge, "invalid_package", "插件包为空或超过 32 MiB")
			return
		}
		meta, err := validatePluginPackage(data)
		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_manifest", err.Error())
			return
		}
		sha, _, _, err := PersistUploadedBlob(r.Context(), s, storageDir, bytes.NewReader(data), "application/zip", r2)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "upload_failed", "插件包保存失败")
			return
		}
		var versionID int64
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			var owner int
			var existingRuntime string
			var existingPermissions json.RawMessage
			ownerErr := tx.QueryRow(r.Context(), `SELECT uploader_uid, runtime, permissions FROM plugins WHERE id=$1 FOR UPDATE`, meta.ID).Scan(&owner, &existingRuntime, &existingPermissions)
			if ownerErr == pgx.ErrNoRows {
				_, ownerErr = tx.Exec(r.Context(), `INSERT INTO plugins(id,name,author,description,uploader_uid,runtime,permissions,state) VALUES($1,$2,$3,$4,$5,$6,$7,'hidden')`, meta.ID, meta.Name, meta.Author, meta.Description, uid, meta.Runtime, meta.Permissions)
			} else if ownerErr == nil {
				if owner != uid {
					return fmt.Errorf("forbidden")
				}
				if existingRuntime != meta.Runtime || !samePluginPermissions(existingPermissions, meta.Permissions) {
					return fmt.Errorf("approved_metadata_immutable")
				}
			}
			if ownerErr != nil && ownerErr != pgx.ErrNoRows {
				return ownerErr
			}
			var state string
			lookupErr := tx.QueryRow(r.Context(), `SELECT state FROM plugin_versions WHERE plugin_id=$1 AND version=$2 FOR UPDATE`, meta.ID, meta.Version).Scan(&state)
			if lookupErr == nil && state == "approved" {
				return fmt.Errorf("approved_version_immutable")
			}
			if lookupErr != nil && lookupErr != pgx.ErrNoRows {
				return lookupErr
			}
			if lookupErr == pgx.ErrNoRows {
				return tx.QueryRow(r.Context(), `INSERT INTO plugin_versions(plugin_id,version,package_sha256,state) VALUES($1,$2,$3,'pending') RETURNING id`, meta.ID, meta.Version, sha).Scan(&versionID)
			}
			return tx.QueryRow(r.Context(), `UPDATE plugin_versions SET package_sha256=$3,state='pending',reviewer_uid=NULL,review_note='',reviewed_at=NULL WHERE plugin_id=$1 AND version=$2 RETURNING id`, meta.ID, meta.Version, sha).Scan(&versionID)
		})
		if err != nil {
			switch {
			case strings.Contains(err.Error(), "forbidden"):
				httputil.Error(w, http.StatusForbidden, "forbidden", "仅插件所有者可上传新版本")
			case strings.Contains(err.Error(), "approved_version_immutable"):
				httputil.Error(w, http.StatusConflict, "version_immutable", "已审核版本不可覆盖")
			case strings.Contains(err.Error(), "approved_metadata_immutable"):
				httputil.Error(w, http.StatusConflict, "metadata_immutable", "已上架插件的运行时和权限不可变更")
			default:
				httputil.Error(w, http.StatusBadRequest, "upload_failed", err.Error())
			}
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"id": meta.ID, "plugin_id": meta.ID, "version_id": versionID, "version": meta.Version, "state": "pending", "package_sha256": sha})
	}
}

// HandleDeletePlugin lets the uploader remove a plugin that has not been
// approved. Approved plugins must go through admin delist/revocation.
func HandleDeletePlugin(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := strings.Trim(r.PathValue("id"), "/")
		if id == "" {
			id = strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/"), "/")
		}
		var approved, owner bool
		err = s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM plugin_versions v JOIN plugins p ON p.current_version_id=v.id WHERE p.id=$1 AND v.state='approved'), EXISTS(SELECT 1 FROM plugins WHERE id=$1 AND uploader_uid=$2)`, id, uid).Scan(&approved, &owner)
		if err != nil {
			httputil.Error(w, 500, "db_error", "读取插件失败")
			return
		}
		if approved {
			httputil.Error(w, 409, "approved_plugin_requires_review", "已审核插件需由管理员下架")
			return
		}
		if !owner {
			httputil.Error(w, http.StatusNotFound, "plugin_not_found", "插件不存在或无权删除")
			return
		}
		result, err := s.Pool.Exec(r.Context(), `DELETE FROM plugins WHERE id=$1 AND uploader_uid=$2`, id, uid)
		if err != nil {
			httputil.Error(w, 500, "delete_failed", "删除插件失败")
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, 404, "plugin_not_found", "插件不存在或无权删除")
			return
		}
		httputil.JSON(w, 200, map[string]string{"status": "deleted", "id": id})
	}
}

type PluginItem struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Author        string          `json:"author"`
	Description   string          `json:"description"`
	UploaderUID   int             `json:"uploader_uid"`
	Runtime       string          `json:"runtime"`
	Permissions   json.RawMessage `json:"permissions"`
	State         string          `json:"state"`
	VersionState  string          `json:"version_state,omitempty"`
	Owned         bool            `json:"owned,omitempty"`
	Version       string          `json:"version,omitempty"`
	PackageSHA256 string          `json:"package_sha256,omitempty"`
}

// HandleGetPlugins implements GET /api/v1/plugins
func HandleGetPlugins(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid := 0
		if token := httputil.ExtractBearerToken(r); token != "" {
			if authenticated, authErr := httputil.AuthenticateUser(r.Context(), s, token); authErr == nil {
				uid = authenticated
			}
		}
		where := `p.state = 'visible' AND pv.state = 'approved'`
		ownedExpr := "false"
		args := []any{}
		if uid > 0 {
			where = fmt.Sprintf(`(%s OR p.uploader_uid = $1)`, where)
			ownedExpr = "p.uploader_uid = $1"
			args = append(args, uid)
		}
		rows, err := s.Pool.Query(r.Context(), `
			SELECT p.id, p.name, p.author, p.description, p.uploader_uid, p.runtime, p.permissions, p.state,
			       COALESCE(pv.state, ''), COALESCE(pv.version, ''), COALESCE(pv.package_sha256, ''),
			       (`+ownedExpr+`)
			FROM plugins p
			LEFT JOIN LATERAL (SELECT state, version, package_sha256 FROM plugin_versions v WHERE v.plugin_id=p.id ORDER BY (v.state IN ('pending','waiting_review')) DESC, v.created_at DESC LIMIT 1) pv ON true
			WHERE `+where+`
			ORDER BY p.created_at DESC
		`, args...)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var list []PluginItem
		for rows.Next() {
			var p PluginItem
			if err := rows.Scan(&p.ID, &p.Name, &p.Author, &p.Description, &p.UploaderUID, &p.Runtime, &p.Permissions, &p.State, &p.VersionState, &p.Version, &p.PackageSHA256, &p.Owned); err == nil {
				if p.Owned && p.VersionState != "" && p.VersionState != "approved" {
					p.State = p.VersionState
				}
				list = append(list, p)
			}
		}
		if list == nil {
			list = []PluginItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

// HandleGetPluginDetail implements GET /api/v1/plugins/{id}
func HandleGetPluginDetail(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/plugins/")
		id = strings.Trim(id, "/")
		uid := 0
		if token := httputil.ExtractBearerToken(r); token != "" {
			if authenticated, authErr := httputil.AuthenticateUser(r.Context(), s, token); authErr == nil {
				uid = authenticated
			}
		}
		where := "p.state = 'visible' AND pv.state = 'approved'"
		ownedExpr := "false"
		args := []any{id}
		if uid > 0 {
			where = fmt.Sprintf("(%s OR p.uploader_uid = $2)", where)
			ownedExpr = "p.uploader_uid = $2"
			args = append(args, uid)
		}
		var p PluginItem
		err := s.Pool.QueryRow(r.Context(), `
			SELECT p.id, p.name, p.author, p.description, p.uploader_uid, p.runtime, p.permissions, p.state,
			       COALESCE(pv.state, ''), COALESCE(pv.version, ''), COALESCE(pv.package_sha256, ''), (`+ownedExpr+`)
			FROM plugins p
			LEFT JOIN LATERAL (SELECT state, version, package_sha256 FROM plugin_versions v WHERE v.plugin_id=p.id ORDER BY (v.state IN ('pending','waiting_review')) DESC, v.created_at DESC LIMIT 1) pv ON true
			WHERE p.id = $1 AND `+where,
			args...).Scan(&p.ID, &p.Name, &p.Author, &p.Description, &p.UploaderUID, &p.Runtime, &p.Permissions, &p.State, &p.VersionState, &p.Version, &p.PackageSHA256, &p.Owned)
		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "plugin_not_found", "插件不存在")
			return
		}
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if p.Owned && p.VersionState != "" && p.VersionState != "approved" {
			p.State = p.VersionState
		}
		httputil.JSON(w, http.StatusOK, p)
	}
}

// HandleDownloadPluginPackage implements GET /api/v1/plugins/:id/package (Spec §4.2)
func HandleDownloadPluginPackage(s *store.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "plugins" && idx+1 < len(parts) {
					id = parts[idx+1]
					break
				}
			}
		}

		var pkgSHA string
		err := s.Pool.QueryRow(r.Context(), `
			SELECT COALESCE(pv.package_sha256, '')
			FROM plugins p
			JOIN plugin_versions pv ON p.current_version_id = pv.id AND pv.state = 'approved'
			WHERE p.id = $1 AND p.state = 'visible'
		`, id).Scan(&pkgSHA)

		if err == pgx.ErrNoRows || pkgSHA == "" {
			httputil.Error(w, http.StatusNotFound, "plugin_not_found", "插件安装包不存在")
			return
		}

		http.Redirect(w, r, fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, pkgSHA), http.StatusFound)
	}
}
