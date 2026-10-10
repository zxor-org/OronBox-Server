package creator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// HandleCreatorGetCollaborators implements GET /api/v1/creator/resources/:id/collaborators (Spec §13.1)
func HandleCreatorGetCollaborators(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		resID := r.PathValue("id")
		if resID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "resources" && idx+1 < len(parts) {
					resID = parts[idx+1]
					break
				}
			}
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT rc.resource_id, rc.user_uid, COALESCE(u.username, ''), rc.role, rc.status,
			       to_char(rc.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM resource_collaborators rc
			JOIN users u ON rc.user_uid = u.bandbbs_uid
			WHERE rc.resource_id = $1
		`, resID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		type CollabItem struct {
			ResourceID string `json:"resource_id"`
			UserUID    int    `json:"user_uid"`
			Username   string `json:"username"`
			Role       string `json:"role"`
			Status     string `json:"status"`
			CreatedAt  string `json:"created_at"`
		}

		var items []CollabItem
		for rows.Next() {
			var it CollabItem
			if err := rows.Scan(&it.ResourceID, &it.UserUID, &it.Username, &it.Role, &it.Status, &it.CreatedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []CollabItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleCreatorInviteCollaborator implements POST /api/v1/creator/resources/:id/collaborators (Spec §13.2)
func HandleCreatorInviteCollaborator(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		resID := r.PathValue("id")
		if resID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "resources" && idx+1 < len(parts) {
					resID = parts[idx+1]
					break
				}
			}
		}

		var req struct {
			BandBBSUID int    `json:"bandbbs_uid"`
			Role       string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.BandBBSUID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误，请提供合法的 bandbbs_uid")
			return
		}
		if req.Role == "" {
			req.Role = "co_author"
		}

		// Verify resource owner
		var ownerUID int
		if err := s.Pool.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, resID).Scan(&ownerUID); err != nil {
			httputil.Error(w, http.StatusNotFound, "resource_not_found", "资源不存在")
			return
		}
		if ownerUID != userUID {
			httputil.Error(w, http.StatusForbidden, "forbidden", "仅资源所有者可邀请协作者")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			INSERT INTO resource_collaborators (resource_id, user_uid, role, status, updated_at)
			VALUES ($1, $2, $3, 'accepted', now())
			ON CONFLICT (resource_id, user_uid) DO UPDATE
			SET role = EXCLUDED.role, status = 'accepted', updated_at = now()
		`, resID, req.BandBBSUID, req.Role)

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "invited"})
	}
}
