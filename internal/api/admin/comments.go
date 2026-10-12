package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type AdminCommentItem struct {
	ID         int64  `json:"id"`
	ResourceID string `json:"resource_id"`
	UserUID    int    `json:"user_uid"`
	Username   string `json:"username"`
	Content    string `json:"content"`
	State      string `json:"state"`
	IsDeleted  bool   `json:"is_deleted"`
	CreatedAt  string `json:"created_at"`
}

// HandleAdminGetComments implements GET /admin/api/comments (Spec §14.6)
func HandleAdminGetComments(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT c.id, c.resource_id, c.user_uid, COALESCE(u.username, ''), c.content, c.state, c.is_deleted,
			       to_char(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM resource_comments c
			JOIN users u ON c.user_uid = u.bandbbs_uid
			ORDER BY c.created_at DESC
			LIMIT 100
		`)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []AdminCommentItem
		for rows.Next() {
			var it AdminCommentItem
			if err := rows.Scan(&it.ID, &it.ResourceID, &it.UserUID, &it.Username, &it.Content, &it.State, &it.IsDeleted, &it.CreatedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []AdminCommentItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleAdminUpdateCommentState implements POST /admin/api/comments/:id (Spec §14.6)
func HandleAdminUpdateCommentState(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		commentID := int64(httputil.ParseInt(parts[len(parts)-1], 0))
		if commentID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "评论ID无效")
			return
		}

		var req struct {
			Action string `json:"action"` // "hide" or "restore"
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}
		if req.Action != "hide" && req.Action != "restore" && req.Action != "delete" {
			httputil.Error(w, http.StatusBadRequest, "invalid_action", "不支持的评论操作")
			return
		}
		var commentUser int
		var resourceID string
		if err := s.Pool.QueryRow(r.Context(), `SELECT user_uid, resource_id FROM resource_comments WHERE id = $1`, commentID).Scan(&commentUser, &resourceID); err != nil {
			httputil.Error(w, http.StatusNotFound, "comment_not_found", "评论不存在")
			return
		}

		state := "visible"
		if req.Action == "hide" {
			state = "hidden"
		}
		deleted := false
		if req.Action == "delete" {
			state = "hidden"
			deleted = true
		}

		result, err := s.Pool.Exec(r.Context(), `
			UPDATE resource_comments SET state = $1, is_deleted = CASE WHEN $3 THEN true ELSE is_deleted END, moderation_reason = $4 WHERE id = $2
		`, state, commentID, deleted, strings.TrimSpace(req.Reason))
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "comment_not_found", "评论不存在")
			return
		}

		targetData, _ := json.Marshal(map[string]any{"comment_id": commentID, "action": req.Action, "reason": req.Reason})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_moderate_comment', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)
		if commentUser > 0 && commentUser != adminUID {
			event, title := "comment.hidden", "评论已被隐藏"
			if req.Action == "restore" {
				event, title = "comment.restored", "评论已恢复显示"
			}
			data, _ := json.Marshal(map[string]any{"automated": false, "reason": strings.TrimSpace(req.Reason), "comment_id": commentID, "resource_id": resourceID})
			_, _ = s.Pool.Exec(r.Context(), `INSERT INTO user_messages (user_uid,kind,event,title,body,ref,data) VALUES ($1,'system',$2,$3,$4,$5,$6)`, commentUser, event, title, strings.TrimSpace(req.Reason), resourceID, data)
		}

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "success", "state": state})
	}
}
