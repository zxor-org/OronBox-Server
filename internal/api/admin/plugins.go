package admin

import (
	"encoding/json"
	"net/http"
	"strings"

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
			LEFT JOIN plugin_versions pv ON p.current_version_id = pv.id
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

		newState := "approved"
		if req.Action == "reject" {
			newState = "rejected"
		}

		_, err = s.Pool.Exec(r.Context(), `
			UPDATE plugins SET state = $1, updated_at = now() WHERE id = $2
		`, newState, pluginID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
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

		newState := "hidden"
		if req.Action == "relist" {
			newState = "visible"
		}

		_, err = s.Pool.Exec(r.Context(), `
			UPDATE plugins SET state = $1, updated_at = now() WHERE id = $2
		`, newState, pluginID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
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
