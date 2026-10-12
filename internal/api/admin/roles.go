package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func HandleGetRoleMembers(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, role, err := httputil.AuthenticateAdminRole(r.Context(), s, r); err != nil || (role != "admin" && role != "superadmin") {
			httputil.Error(w, http.StatusForbidden, "forbidden", "权限不足")
			return
		}
		rows, err := s.Pool.Query(r.Context(), `SELECT bandbbs_uid, username, role, banned FROM users WHERE role IN ('superadmin','admin','reviewer') ORDER BY role, bandbbs_uid`)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", "查询失败")
			return
		}
		defer rows.Close()
		members := make([]map[string]any, 0)
		for rows.Next() {
			var uid int
			var username, role string
			var banned bool
			if rows.Scan(&uid, &username, &role, &banned) == nil {
				members = append(members, map[string]any{"bandbbs_uid": uid, "username": username, "role": role, "banned": banned})
			}
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"items": members})
	}
}

func HandleSetAdminRole(s *store.Store, protected ...[]string) http.HandlerFunc {
	return handleSetRole(s, "admin", "superadmin", protected...)
}

func HandleSetReviewerRole(s *store.Store, protected ...[]string) http.HandlerFunc {
	return handleSetRole(s, "reviewer", "admin", protected...)
}

func handleSetRole(s *store.Store, targetRole, minimumRole string, protected ...[]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actorUID, _, actorRole, err := httputil.AuthenticateAdminRole(r.Context(), s, r)
		if err != nil || (minimumRole == "superadmin" && actorRole != "superadmin") || (minimumRole == "admin" && actorRole != "admin" && actorRole != "superadmin") {
			httputil.Error(w, http.StatusForbidden, "forbidden", "权限不足")
			return
		}
		var req struct {
			UID    int    `json:"bandbbs_uid"`
			Action string `json:"action"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || req.UID <= 0 || (req.Action != "grant" && req.Action != "revoke") {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}
		var currentRole string
		if err := s.Pool.QueryRow(r.Context(), `SELECT role FROM users WHERE bandbbs_uid = $1`, req.UID).Scan(&currentRole); err != nil {
			httputil.Error(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		for _, group := range protected {
			for _, uid := range group {
				if uid == strconv.Itoa(req.UID) {
					httputil.Error(w, http.StatusConflict, "role_protected", "环境变量中的超级管理员不可修改")
					return
				}
			}
		}
		if currentRole == "superadmin" || (targetRole == "reviewer" && currentRole == "admin") || (targetRole == "admin" && currentRole == "reviewer" && req.Action == "grant") {
			httputil.Error(w, http.StatusConflict, "role_protected", "该角色不可通过此接口修改")
			return
		}
		newRole := "user"
		if req.Action == "grant" {
			newRole = targetRole
		}
		if _, err := s.Pool.Exec(r.Context(), `UPDATE users SET role = $1, updated_at = now() WHERE bandbbs_uid = $2 AND role = $3`, newRole, req.UID, currentRole); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", "更新失败")
			return
		}
		target, _ := json.Marshal(map[string]any{"bandbbs_uid": req.UID, "action": req.Action, "role": targetRole})
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO audit_logs (actor_uid, action, result, ip, target_data) VALUES ($1, 'set_role', 'success', $2, $3)`, actorUID, r.RemoteAddr, target)
		httputil.JSON(w, http.StatusOK, map[string]any{"bandbbs_uid": req.UID, "role": newRole})
	}
}
