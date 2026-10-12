package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/api/catalog"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type AdminPluginItem struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Author        string          `json:"author"`
	Description   string          `json:"description"`
	UploaderUID   int             `json:"uploader_uid"`
	Runtime       string          `json:"runtime"`
	Permissions   json.RawMessage `json:"permissions"`
	State         string          `json:"state"`
	Version       string          `json:"version"`
	PackageSHA256 string          `json:"package_sha256"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

// HandleAdminGetPlugins implements GET /admin/api/plugins (Spec §14.4)
func HandleAdminGetPlugins(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT p.id, p.name, p.author, p.description, p.uploader_uid, p.runtime, p.permissions, p.state,
			       COALESCE(pv.version, ''), COALESCE(pv.package_sha256, ''),
			       to_char(p.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       to_char(p.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM plugins p
			LEFT JOIN LATERAL (SELECT * FROM plugin_versions v WHERE v.plugin_id=p.id ORDER BY (v.state IN ('pending','waiting_review')) DESC, v.created_at DESC LIMIT 1) pv ON true
			ORDER BY p.updated_at DESC
		`)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []AdminPluginItem
		for rows.Next() {
			var it AdminPluginItem
			if err := rows.Scan(&it.ID, &it.Name, &it.Author, &it.Description, &it.UploaderUID, &it.Runtime, &it.Permissions, &it.State, &it.Version, &it.PackageSHA256, &it.CreatedAt, &it.UpdatedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []AdminPluginItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleAdminDownloadPlugin permits reviewing pending packages with the isolated
// administrator session, without requiring client attestation headers.
func HandleAdminDownloadPlugin(s *store.Store, blobDir, r2PublicURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := httputil.AuthenticateAdmin(r.Context(), s, r); err != nil {
			httputil.Error(w, 401, "unauthorized", "未授权")
			return
		}
		id := r.PathValue("id")
		var sha string
		err := s.Pool.QueryRow(r.Context(), `SELECT package_sha256 FROM plugin_versions WHERE plugin_id=$1 ORDER BY (state IN ('pending','waiting_review')) DESC, created_at DESC LIMIT 1`, id).Scan(&sha)
		if err != nil || sha == "" {
			httputil.Error(w, 404, "package_not_found", "插件安装包不存在")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		clone := r.Clone(r.Context())
		clone.URL.Path = "/api/v1/blobs/" + sha
		catalog.HandleServeBlob(s, blobDir, r2PublicURL)(w, clone)
	}
}

// HandleAdminReviewPlugin implements POST /admin/api/plugins/:id/review (Spec §14.4)
func HandleAdminReviewPlugin(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		var pluginID string
		for idx, p := range parts {
			if p == "plugins" && idx+1 < len(parts) {
				pluginID = parts[idx+1]
				break
			}
		}

		var req struct {
			Action string `json:"action"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}

		if req.Action != "approve" && req.Action != "reject" {
			httputil.Error(w, http.StatusBadRequest, "invalid_action", "不支持的审核动作")
			return
		}
		newState := "approved"
		versionState := "approved"
		if req.Action == "reject" {
			newState, versionState = "rejected", "rejected"
		}
		result, err := s.Pool.Exec(r.Context(), `
			WITH target AS (
				SELECT id FROM plugin_versions
				WHERE plugin_id = $2 AND state IN ('pending', 'waiting_review')
				  AND ($1 = 'rejected' OR (package_sha256 ~ '^[0-9a-fA-F]{64}$'
				       AND EXISTS (SELECT 1 FROM blobs b WHERE b.sha256 = plugin_versions.package_sha256)))
				ORDER BY created_at DESC LIMIT 1
			), updated_version AS (
				UPDATE plugin_versions pv SET state = $1, reviewer_uid = $3, review_note = $4, reviewed_at = now()
				FROM target WHERE pv.id = target.id RETURNING pv.id
			)
			UPDATE plugins p SET state = CASE WHEN $1 = 'approved' THEN 'visible' WHEN p.current_version_id IS NULL THEN 'hidden' ELSE p.state END,
				current_version_id = CASE WHEN $1 = 'approved' THEN (SELECT id FROM updated_version) ELSE p.current_version_id END,
				updated_at = now() WHERE p.id = $2 AND EXISTS (SELECT 1 FROM updated_version)
		`, versionState, pluginID, adminUID, req.Reason)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusConflict, "version_not_pending", "没有可审核的待审插件版本")
			return
		}

		targetData, _ := json.Marshal(map[string]any{"plugin_id": pluginID, "action": req.Action, "reason": req.Reason})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_review_plugin', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "success", "state": newState})
	}
}

// HandleAdminStatePlugin implements POST /admin/api/plugins/:id/state (Spec §14.4)
func HandleAdminStatePlugin(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		var pluginID string
		for idx, p := range parts {
			if p == "plugins" && idx+1 < len(parts) {
				pluginID = parts[idx+1]
				break
			}
		}

		var req struct {
			Action string `json:"action"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}

		if req.Action != "hide" && req.Action != "delist" && req.Action != "relist" {
			httputil.Error(w, http.StatusBadRequest, "invalid_action", "不支持的状态动作")
			return
		}
		newState := "hidden"
		if req.Action == "relist" {
			newState = "visible"
		}
		result, err := s.Pool.Exec(r.Context(), `
			UPDATE plugins SET state = $1, updated_at = now()
			WHERE id = $2 AND ($1 = 'hidden' OR EXISTS (
				SELECT 1 FROM plugin_versions pv WHERE pv.id = plugins.current_version_id AND pv.state = 'approved'
			))
		`, newState, pluginID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "plugin_not_found", "插件不存在或没有可见版本")
			return
		}

		targetData, _ := json.Marshal(map[string]any{"plugin_id": pluginID, "action": req.Action, "reason": req.Reason})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_state_plugin', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "success", "state": newState})
	}
}
