package creator

import (
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// HandleGetCollaborationInvitations lists pending invitations for the signed
// in BandBBS user. The creator client uses this to render the accept/decline
// flow instead of treating an invite as silently accepted.
func HandleGetCollaborationInvitations(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		rows, err := s.Pool.Query(r.Context(), `SELECT c.resource_id,c.user_uid,c.role,c.status,to_char(c.created_at,'YYYY-MM-DD"T"HH24:MI:SSZ'),r.title,COALESCE(u.username,'') FROM resource_collaborators c JOIN resources r ON r.resource_id=c.resource_id LEFT JOIN users u ON u.bandbbs_uid=r.owner_uid WHERE c.user_uid=$1 AND c.status='pending' ORDER BY c.created_at DESC`, uid)
		if err != nil {
			httputil.Error(w, 500, "db_error", "读取协作邀请失败")
			return
		}
		defer rows.Close()
		items := make([]map[string]any, 0)
		for rows.Next() {
			var resourceID, role, status, createdAt, title, owner string
			var invitedUID int
			if rows.Scan(&resourceID, &invitedUID, &role, &status, &createdAt, &title, &owner) == nil {
				items = append(items, map[string]any{"resource_id": resourceID, "user_uid": invitedUID, "role": role, "status": status, "created_at": createdAt, "resource_title": title, "owner_username": owner})
			}
		}
		httputil.JSON(w, 200, map[string]any{"invitations": items, "items": items})
	}
}

func collaborationResourceID(r *http.Request) string {
	if id := r.PathValue("id"); id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, p := range parts {
		if p == "resources" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func HandleAcceptCollaboration(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := collaborationResourceID(r)
		result, err := s.Pool.Exec(r.Context(), `UPDATE resource_collaborators SET status='accepted',updated_at=now() WHERE resource_id=$1 AND user_uid=$2 AND status='pending'`, id, uid)
		if err != nil {
			httputil.Error(w, 500, "db_error", "接受协作邀请失败")
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, 404, "invitation_not_found", "协作邀请不存在或已处理")
			return
		}
		httputil.JSON(w, 200, map[string]string{"status": "accepted"})
	}
}

func HandleDeclineCollaboration(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := collaborationResourceID(r)
		result, err := s.Pool.Exec(r.Context(), `DELETE FROM resource_collaborators WHERE resource_id=$1 AND user_uid=$2 AND status='pending'`, id, uid)
		if err != nil {
			httputil.Error(w, 500, "db_error", "拒绝协作邀请失败")
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, 404, "invitation_not_found", "协作邀请不存在或已处理")
			return
		}
		httputil.JSON(w, 200, map[string]string{"status": "declined"})
	}
}
