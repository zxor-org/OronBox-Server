package server

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"
)

func (a *application) adminContract(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/admin/api/analytics") && r.Method == "GET":
		a.adminAnalytics(w, r)
	case strings.HasPrefix(p, "/admin/api/blogs") && r.Method == "GET":
		a.adminBlogs(w, r)
	case strings.HasPrefix(p, "/admin/api/blogs") && (r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE"):
		a.adminBlogMutation(w, r)
	case strings.Contains(p, "/reviews/") && strings.HasSuffix(p, "/detail") && r.Method == "GET":
		a.adminReviewDetail(w, r)
	case strings.HasPrefix(p, "/admin/api/resources/") && r.Method == "GET":
		a.adminResourceDetail(w, r)
	case strings.HasPrefix(p, "/admin/api/resources/") && (r.Method == "PUT" || r.Method == "POST"):
		a.adminResourceUpdate(w, r)
	case strings.HasPrefix(p, "/admin/api/plugins") && strings.HasSuffix(p, "/review") && r.Method == "POST":
		a.adminPluginReview(w, r)
	case strings.HasPrefix(p, "/admin/api/plugins") && strings.HasSuffix(p, "/state") && r.Method == "POST":
		a.adminPluginState(w, r)
	case strings.HasPrefix(p, "/admin/api/plugins") && strings.HasSuffix(p, "/download") && r.Method == "GET":
		a.adminPluginDownload(w, r)
	case strings.HasPrefix(p, "/admin/api/plugins") && r.Method == "GET":
		a.adminPlugins(w, r)
	case strings.HasPrefix(p, "/admin/api/resources") && r.Method == "GET":
		a.adminResources(w, r)
	case strings.HasPrefix(p, "/admin/api/reviews") && r.Method == "GET":
		a.adminReviews(w, r)
	case strings.HasPrefix(p, "/admin/api/reviews") && r.Method == "POST":
		a.adminReviewMutation(w, r)
	case strings.HasPrefix(p, "/admin/api/comments") && r.Method == "GET":
		a.adminComments(w, r)
	case strings.HasPrefix(p, "/admin/api/comments/bulk") && r.Method == "POST":
		a.adminCommentsBulk(w, r)
	case strings.HasPrefix(p, "/admin/api/users") && r.Method == "GET":
		a.adminUsers(w, r)
	case strings.HasSuffix(p, "/sessions") && r.Method == "POST":
		parts := strings.Split(strings.Trim(p, "/"), "/")
		if len(parts) < 4 {
			jsonError(w, 404, "not_found", "user not found", nil)
			return
		}
		target := parts[len(parts)-2]
		_, e := a.db.Pool.Exec(r.Context(), `
			UPDATE sessions 
			SET revoked_at=now() 
			WHERE user_id IN (
				SELECT id FROM users 
				WHERE id=$1 OR (CASE WHEN $1 ~ '^[0-9]+$' THEN bandbbs_uid=$1::bigint ELSE false END)
			) AND revoked_at IS NULL
		`, target)
		if e != nil {
			jsonError(w, 500, "session_revoke_failed", e.Error(), nil)
			return
		}
		jsonResponse(w, 200, map[string]any{"ok": true})
	case strings.HasPrefix(p, "/admin/api/tickets") && r.Method == "GET":
		a.adminTickets(w, r)
	case strings.HasPrefix(p, "/admin/api/tickets/") && r.Method == "POST":
		a.adminTicketMutation(w, r)
	case strings.HasPrefix(p, "/admin/api/publications") && r.Method == "GET":
		a.adminPublications(w, r)
	case strings.HasPrefix(p, "/admin/api/publications/") && r.Method == "POST":
		a.adminPublicationRetry(w, r)
	case p == "/admin/api/coins" && r.Method == "GET":
		a.adminCoins(w, r)
	case strings.HasPrefix(p, "/admin/api/coins/ledger") && r.Method == "GET":
		a.adminLedger(w, r)
	case strings.HasPrefix(p, "/admin/api/audit") && r.Method == "GET":
		a.adminAudit(w, r)
	case strings.HasPrefix(p, "/admin/api/templates") && r.Method == "GET":
		a.adminModerationTemplates(w, r)
	case strings.HasPrefix(p, "/admin/api/templates") && (r.Method == "POST" || r.Method == "DELETE"):
		a.adminModerationTemplateMutation(w, r)
	case strings.HasSuffix(p, "/settings") && r.Method == "GET":
		rows, e := a.db.Pool.Query(r.Context(), `SELECT key,value FROM server_settings ORDER BY key`)
		if e != nil {
			jsonError(w, 500, "settings_failed", e.Error(), nil)
			return
		}
		defer rows.Close()
		out := map[string]any{}
		for rows.Next() {
			var k string
			var raw []byte
			if rows.Scan(&k, &raw) == nil {
				var v any
				if json.Unmarshal(raw, &v) != nil {
					v = map[string]any{}
				}
				out[k] = v
			}
		}
		jsonResponse(w, 200, map[string]any{"settings": out})
	case strings.HasSuffix(p, "/settings") && r.Method == "POST":
		var in map[string]any
		if !readJSON(w, r, &in) {
			return
		}
		for k, v := range in {
			raw, _ := json.Marshal(v)
			if _, e := a.db.Pool.Exec(r.Context(), `INSERT INTO server_settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=now()`, k, raw); e != nil {
				jsonError(w, 500, "settings_failed", e.Error(), nil)
				return
			}
		}
		jsonResponse(w, 200, map[string]string{"status": "updated"})
	case strings.HasSuffix(p, "/messages/send") && r.Method == "POST":
		var in struct {
			UserIDs []string `json:"user_ids"`
			Title   string   `json:"title"`
			Body    string   `json:"body"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		count := 0
		for _, id := range in.UserIDs {
			if _, e := a.db.Pool.Exec(r.Context(), `INSERT INTO user_messages(user_id,kind,event,title,body) VALUES($1,'admin_direct','admin.message',$2,$3)`, id, in.Title, in.Body); e == nil {
				count++
			}
		}
		jsonResponse(w, 200, map[string]any{"ok": true, "count": count})
	case strings.Contains(p, "/users/") && strings.HasSuffix(p, "/state") && r.Method == "POST":
		parts := strings.Split(strings.Trim(p, "/"), "/")
		target := parts[len(parts)-2]
		var in struct {
			Role   string `json:"role"`
			Banned bool   `json:"banned"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		res, e := a.db.Pool.Exec(r.Context(), `
			UPDATE users 
			SET role=$1, banned=$2, updated_at=now() 
			WHERE id=$3 OR (CASE WHEN $3 ~ '^[0-9]+$' THEN bandbbs_uid=$3::bigint ELSE false END)
		`, in.Role, in.Banned, target)
		if e != nil {
			jsonError(w, 500, "user_state_failed", e.Error(), nil)
			return
		}
		if res.RowsAffected() == 0 {
			jsonError(w, 404, "user_not_found", "user not found", nil)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": "updated"})
	case strings.Contains(p, "/comments/") && r.Method == "POST":
		parts := strings.Split(strings.Trim(p, "/"), "/")
		id := parts[len(parts)-1]
		var in struct {
			Action string `json:"action"`
			Reason string `json:"reason"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		state := "visible"
		if in.Action == "hide" {
			state = "hidden"
		}
		_, e := a.db.Pool.Exec(r.Context(), `UPDATE resource_comments SET state=$1,is_deleted=($1='hidden'),moderation_reason=$2 WHERE id=$3`, state, in.Reason, id)
		if e != nil {
			jsonError(w, 500, "comment_moderation_failed", e.Error(), nil)
			return
		}
		jsonResponse(w, 200, map[string]string{"status": state})
	case strings.Contains(p, "/coins/users/") && r.Method == "POST":
		parts := strings.Split(strings.Trim(p, "/"), "/")
		target := parts[len(parts)-1]
		var in struct {
			Delta int64  `json:"delta"`
			Note  string `json:"note"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		// Resolve real internal UUID if target is bandbbs_uid
		var realUserID string
		err := a.db.Pool.QueryRow(r.Context(), `
			SELECT id FROM users 
			WHERE id=$1 OR (CASE WHEN $1 ~ '^[0-9]+$' THEN bandbbs_uid=$1::bigint ELSE false END) 
			LIMIT 1
		`, target).Scan(&realUserID)
		if err != nil {
			jsonError(w, 404, "user_not_found", "user not found", nil)
			return
		}
		if e := a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
			if _, e := tx.Exec(r.Context(), `INSERT INTO user_coin_accounts(user_id,balance) VALUES($1,0) ON CONFLICT DO NOTHING`, realUserID); e != nil {
				return e
			}
			if _, e := tx.Exec(r.Context(), `UPDATE user_coin_accounts SET balance=balance+$1,updated_at=now() WHERE user_id=$2`, in.Delta, realUserID); e != nil {
				return e
			}
			var balance int64
			if e := tx.QueryRow(r.Context(), `SELECT balance FROM user_coin_accounts WHERE user_id=$1 FOR UPDATE`, realUserID).Scan(&balance); e != nil {
				return e
			}
			_, e := tx.Exec(r.Context(), `INSERT INTO coin_ledger(user_id,delta_units,kind,reference_type,note,balance_after) VALUES($1,$2,'admin_adjustment','admin',$3,$4)`, realUserID, in.Delta, in.Note, balance)
			return e
		}); e != nil {
			jsonError(w, 500, "coin_adjustment_failed", e.Error(), nil)
			return
		}
		jsonResponse(w, 200, map[string]any{"ok": true, "delta": in.Delta})
	case strings.Contains(p, "/reviews/") && strings.HasSuffix(p, "/decision") && r.Method == "POST":
		var in struct {
			Action string `json:"action"`
			Note   string `json:"note"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		state := "approved"
		if in.Action == "reject" {
			state = "rejected"
		}
		jsonResponse(w, 200, map[string]string{"status": state})
	case r.Method == "GET":
		jsonError(w, 404, "not_found", "admin endpoint not found", nil)
	default:
		jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
	}
}
