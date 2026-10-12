package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type AdminResItem struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Kind            string          `json:"kind"`
	Summary         string          `json:"summary"`
	Authors         json.RawMessage `json:"authors"`
	OwnerUID        int             `json:"owner_uid"`
	Devices         json.RawMessage `json:"devices"`
	Attributes      json.RawMessage `json:"attributes"`
	PaidType        string          `json:"paid_type"`
	IconSHA256      string          `json:"icon_sha256"`
	CoverSHA256     string          `json:"cover_sha256"`
	ModerationState string          `json:"moderation_state"`
	DownloadCount   int             `json:"download_count"`
	CoinCount       int             `json:"coin_count"`
	Rating          float64         `json:"rating"`
	RatingCount     int             `json:"rating_count"`
	PublishedAt     string          `json:"published_at"`
}

// HandleAdminGetResources implements GET /admin/api/resources
func HandleAdminGetResources(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "管理员身份验证失败")
			return
		}

		q := strings.TrimSpace(r.URL.Query().Get("q"))
		page := httputil.ParseInt(r.URL.Query().Get("page"), 1)
		size := httputil.ParseInt(r.URL.Query().Get("size"), 50)
		offset := (page - 1) * size

		query := `SELECT resource_id, title, restype, summary, authors, owner_uid, devices, tags, paid_type, 
		                 icon_sha256, cover_sha256, moderation_state, download_count, coin_count, rating, rating_count,
		                 COALESCE(to_char(published_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), '')
		          FROM resources WHERE 1=1`
		countQuery := `SELECT count(*) FROM resources WHERE 1=1`
		var args []any
		if q != "" {
			query += ` AND (title ILIKE $1 OR resource_id ILIKE $1)`
			countQuery += ` AND (title ILIKE $1 OR resource_id ILIKE $1)`
			args = append(args, "%"+q+"%")
		}

		var total int
		_ = s.Pool.QueryRow(r.Context(), countQuery, args...).Scan(&total)

		idx1 := len(args) + 1
		idx2 := len(args) + 2
		query += fmt.Sprintf(` ORDER BY last_released_at DESC NULLS LAST LIMIT $%d OFFSET $%d`, idx1, idx2)
		args = append(args, size, offset)

		rows, err := s.Pool.Query(r.Context(), query, args...)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []AdminResItem
		for rows.Next() {
			var it AdminResItem
			if err := rows.Scan(&it.ID, &it.Name, &it.Kind, &it.Summary, &it.Authors, &it.OwnerUID,
				&it.Devices, &it.Attributes, &it.PaidType, &it.IconSHA256, &it.CoverSHA256, &it.ModerationState,
				&it.DownloadCount, &it.CoinCount, &it.Rating, &it.RatingCount, &it.PublishedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []AdminResItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"items": items,
			"total": total,
			"page":  page,
			"size":  size,
		})
	}
}

// HandleAdminUpdateResourceState implements POST /admin/api/resources/:id/state
func HandleAdminUpdateResourceState(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		id := strings.TrimPrefix(r.URL.Path, "/admin/api/resources/")
		id = strings.TrimSuffix(id, "/state")

		var req struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}
		if req.State != "visible" && req.State != "suspended" && req.State != "deleted" {
			httputil.Error(w, http.StatusBadRequest, "invalid_state", "资源状态无效")
			return
		}

		result, err := s.Pool.Exec(r.Context(), `
			UPDATE resources 
			SET moderation_state = $1, moderation_reason = $2, moderation_by = 'admin', updated_at = now()
			WHERE resource_id = $3 AND ($1 <> 'visible' OR current_revision_id IS NOT NULL)
		`, req.State, req.Reason, id)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "resource_not_found", "资源不存在")
			return
		}

		targetData, _ := json.Marshal(map[string]any{"resource_id": id, "state": req.State, "reason": req.Reason})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'update_resource_state', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]any{"status": "success"})
	}
}

// HandleAdminUpdateResource implements PUT /admin/api/resources/:id (Spec §14.3)
func HandleAdminUpdateResource(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		id := parts[len(parts)-1]
		if id == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "缺少资源ID")
			return
		}

		var req struct {
			Title           *string `json:"title"`
			Summary         *string `json:"summary"`
			Description     *string `json:"description"`
			Restype         *string `json:"restype"`
			ModerationState *string `json:"moderation_state"`
			PaidType        *string `json:"paid_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			UPDATE resources 
			SET title = COALESCE($1, title),
			    summary = COALESCE($2, summary),
			    description = COALESCE($3, description),
			    restype = COALESCE($4, restype),
			    moderation_state = COALESCE($5, moderation_state),
			    paid_type = COALESCE($6, paid_type),
			    updated_at = now()
			WHERE resource_id = $7
		`, req.Title, req.Summary, req.Description, req.Restype, req.ModerationState, req.PaidType, id)

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		targetData, _ := json.Marshal(map[string]any{"resource_id": id})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_update_resource', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}
