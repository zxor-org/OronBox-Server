package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type AdminTagItem struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Coefficient float64 `json:"coefficient"`
	UsageCount  int     `json:"usage_count"`
	SortOrder   int     `json:"sort_order"`
	Enabled     bool    `json:"enabled"`
}

// HandleAdminGetTags implements GET /admin/api/tags (Spec §14.8)
func HandleAdminGetTags(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT id, name, coefficient, usage_count, sort_order, enabled
			FROM tags
			ORDER BY sort_order ASC, usage_count DESC
		`)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []AdminTagItem
		for rows.Next() {
			var it AdminTagItem
			if err := rows.Scan(&it.ID, &it.Name, &it.Coefficient, &it.UsageCount, &it.SortOrder, &it.Enabled); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []AdminTagItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleAdminCreateTag implements POST /admin/api/tags (Spec §14.8)
func HandleAdminCreateTag(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		var req struct {
			Name        string  `json:"name"`
			Coefficient float64 `json:"coefficient"`
			SortOrder   int     `json:"sort_order"`
			Enabled     bool    `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "缺少标签名称")
			return
		}
		if req.Coefficient <= 0 {
			req.Coefficient = 1.0
		}

		var tagID int
		err = s.Pool.QueryRow(r.Context(), `
			INSERT INTO tags (name, coefficient, sort_order, enabled, usage_count)
			VALUES ($1, $2, $3, $4, 0)
			RETURNING id
		`, req.Name, req.Coefficient, req.SortOrder, req.Enabled).Scan(&tagID)

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "create_tag_failed", "标签名称重复或创建失败")
			return
		}

		targetData, _ := json.Marshal(map[string]any{"id": tagID, "name": req.Name})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_create_tag', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]any{"id": tagID, "status": "created"})
	}
}

// HandleAdminUpdateTag implements PUT /admin/api/tags/:id (Spec §14.8)
func HandleAdminUpdateTag(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		tagID := httputil.ParseInt(parts[len(parts)-1], 0)
		if tagID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "标签ID无效")
			return
		}

		var req struct {
			Name        *string  `json:"name"`
			Coefficient *float64 `json:"coefficient"`
			SortOrder   *int     `json:"sort_order"`
			Enabled     *bool    `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			UPDATE tags 
			SET name = COALESCE($1, name),
			    coefficient = COALESCE($2, coefficient),
			    sort_order = COALESCE($3, sort_order),
			    enabled = COALESCE($4, enabled)
			WHERE id = $5
		`, req.Name, req.Coefficient, req.SortOrder, req.Enabled, tagID)

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		targetData, _ := json.Marshal(map[string]any{"id": tagID})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_update_tag', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// HandleAdminDeleteTag implements DELETE /admin/api/tags/:id (Spec §14.8)
func HandleAdminDeleteTag(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		tagID := httputil.ParseInt(parts[len(parts)-1], 0)
		if tagID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "标签ID无效")
			return
		}

		res, err := s.Pool.Exec(r.Context(), `
			DELETE FROM tags WHERE id = $1 AND usage_count = 0
		`, tagID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if res.RowsAffected() == 0 {
			httputil.Error(w, http.StatusBadRequest, "tag_in_use", "仅允许删除引用计数为 0 的标签")
			return
		}

		targetData, _ := json.Marshal(map[string]any{"id": tagID})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_delete_tag', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
