package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type AdminUserItem struct {
	BandBBSUID int    `json:"bandbbs_uid"`
	Username   string `json:"username"`
	AvatarURL  string `json:"avatar_url"`
	Role       string `json:"role"`
	Banned     bool   `json:"banned"`
	Balance    int    `json:"balance"`
	CreatedAt  string `json:"created_at"`
}

// HandleAdminGetUsers implements GET /admin/api/users (Spec §14.5)
func HandleAdminGetUsers(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		q := strings.TrimSpace(r.URL.Query().Get("q"))
		page := httputil.ParseInt(r.URL.Query().Get("page"), 1)
		size := httputil.ParseInt(r.URL.Query().Get("size"), 50)
		offset := (page - 1) * size

		query := `SELECT u.bandbbs_uid, u.username, COALESCE(u.avatar_url, ''), u.role, u.banned, 
		                 COALESCE(c.balance, 0), to_char(u.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
		          FROM users u
		          LEFT JOIN user_coin_accounts c ON u.bandbbs_uid = c.user_uid
		          WHERE 1=1`
		countQuery := `SELECT count(*) FROM users WHERE 1=1`
		var args []any
		if q != "" {
			query += ` AND (u.username ILIKE $1 OR cast(u.bandbbs_uid as text) ILIKE $1)`
			countQuery += ` AND (username ILIKE $1 OR cast(bandbbs_uid as text) ILIKE $1)`
			args = append(args, "%"+q+"%")
		}

		var total int
		_ = s.Pool.QueryRow(r.Context(), countQuery, args...).Scan(&total)

		idx1 := len(args) + 1
		idx2 := len(args) + 2
		query += fmt.Sprintf(` ORDER BY u.created_at DESC LIMIT $%d OFFSET $%d`, idx1, idx2)
		args = append(args, size, offset)

		rows, err := s.Pool.Query(r.Context(), query, args...)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var list []AdminUserItem
		for rows.Next() {
			var it AdminUserItem
			if err := rows.Scan(&it.BandBBSUID, &it.Username, &it.AvatarURL, &it.Role, &it.Banned, &it.Balance, &it.CreatedAt); err == nil {
				list = append(list, it)
			}
		}
		if list == nil {
			list = []AdminUserItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"items": list,
			"total": total,
			"page":  page,
			"size":  size,
		})
	}
}

// HandleAdminUpdateUserState implements POST /admin/api/users/:uid/state (Spec §14.5)
func HandleAdminUpdateUserState(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		var targetUID int
		for idx, p := range parts {
			if p == "users" && idx+1 < len(parts) {
				targetUID = httputil.ParseInt(parts[idx+1], 0)
				break
			}
		}
		if targetUID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_uid", "用户UID无效")
			return
		}

		var req struct {
			Banned bool   `json:"banned"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			UPDATE users SET banned = $1, updated_at = now() WHERE bandbbs_uid = $2
		`, req.Banned, targetUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		targetData, _ := json.Marshal(map[string]any{"user_uid": targetUID, "banned": req.Banned, "reason": req.Reason})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_update_user_state', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]any{"status": "success", "banned": req.Banned})
	}
}

// HandleAdminKickUserSessions implements POST /admin/api/users/:uid/sessions (Spec §14.5)
func HandleAdminKickUserSessions(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		var targetUID int
		for idx, p := range parts {
			if p == "users" && idx+1 < len(parts) {
				targetUID = httputil.ParseInt(parts[idx+1], 0)
				break
			}
		}
		if targetUID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_uid", "用户UID无效")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			DELETE FROM sessions WHERE user_uid = $1
		`, targetUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		targetData, _ := json.Marshal(map[string]any{"user_uid": targetUID})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_kick_user_sessions', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "sessions_cleared"})
	}
}

// HandleAdminAdjustCoins implements POST /admin/api/coins/users/:uid
func HandleAdminAdjustCoins(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		uid := 0
		for idx, p := range parts {
			if p == "users" && idx+1 < len(parts) {
				uid = httputil.ParseInt(parts[idx+1], 0)
				break
			}
		}
		if uid <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_uid", "无效的用户UID")
			return
		}

		var req struct {
			DeltaUnits int    `json:"delta_units"`
			Reason     string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}

		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			var cur int
			_ = tx.QueryRow(r.Context(), `SELECT balance FROM user_coin_accounts WHERE user_uid = $1 FOR UPDATE`, uid).Scan(&cur)
			newBal := cur + req.DeltaUnits
			if newBal < 0 {
				newBal = 0
			}

			_, err := tx.Exec(r.Context(), `
				INSERT INTO user_coin_accounts (user_uid, balance, updated_at)
				VALUES ($1, $2, now())
				ON CONFLICT (user_uid) DO UPDATE SET balance = $2, updated_at = now()
			`, uid, newBal)
			if err != nil {
				return err
			}

			_, err = tx.Exec(r.Context(), `
				INSERT INTO coin_ledger (user_uid, delta_units, kind, reference_type, note, balance_after)
				VALUES ($1, $2, 'admin_adjust', 'system', $3, $4)
			`, uid, req.DeltaUnits, req.Reason, newBal)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		targetData, _ := json.Marshal(map[string]any{"user_uid": uid, "delta_units": req.DeltaUnits, "reason": req.Reason})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'admin_adjust_coins', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]any{"status": "success"})
	}
}
