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

type AdminTicketItem struct {
	ID         int64  `json:"id"`
	UserUID    int    `json:"user_uid"`
	Username   string `json:"username"`
	Kind       string `json:"kind"`
	Subject    string `json:"subject"`
	Message    string `json:"message"`
	Status     string `json:"status"`
	Resolution string `json:"resolution"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

// HandleAdminGetTicketDetail implements GET /admin/api/tickets/:id.
func HandleAdminGetTicketDetail(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := httputil.AuthenticateAdmin(r.Context(), s, r); err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		id := int64(0)
		for i, part := range parts {
			if part == "tickets" && i+1 < len(parts) {
				id = int64(httputil.ParseInt(parts[i+1], 0))
				break
			}
		}
		if id <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "工单ID无效")
			return
		}
		var ticket AdminTicketItem
		err := s.Pool.QueryRow(r.Context(), `
			SELECT t.id, t.user_uid, COALESCE(u.username, ''), t.kind, t.subject, t.message,
			       t.status, t.resolution,
			       to_char(t.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       to_char(t.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM feedback_tickets t JOIN users u ON t.user_uid = u.bandbbs_uid
			WHERE t.id = $1
		`, id).Scan(&ticket.ID, &ticket.UserUID, &ticket.Username, &ticket.Kind, &ticket.Subject,
			&ticket.Message, &ticket.Status, &ticket.Resolution, &ticket.CreatedAt, &ticket.UpdatedAt)
		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "ticket_not_found", "工单不存在")
			return
		}
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		rows, err := s.Pool.Query(r.Context(), `
			SELECT r.id, r.author_uid, COALESCE(u.username, ''), r.message, r.is_admin,
			       to_char(r.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM feedback_replies r LEFT JOIN users u ON r.author_uid = u.bandbbs_uid
			WHERE r.ticket_id = $1 ORDER BY r.created_at ASC
		`, id)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()
		replies := make([]map[string]any, 0)
		for rows.Next() {
			var replyID int64
			var authorUID int
			var username, message, createdAt string
			var isAdmin bool
			if rows.Scan(&replyID, &authorUID, &username, &message, &isAdmin, &createdAt) == nil {
				replies = append(replies, map[string]any{
					"id": replyID, "author_uid": authorUID, "username": username,
					"message": message, "is_admin": isAdmin, "created_at": createdAt,
				})
			}
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"ticket": ticket, "replies": replies})
	}
}

// HandleAdminGetTickets implements GET /admin/api/tickets (Spec §14.6)
func HandleAdminGetTickets(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT t.id, t.user_uid, COALESCE(u.username, ''), t.kind, t.subject, t.message, t.status, t.resolution,
			       to_char(t.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       to_char(t.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM feedback_tickets t
			JOIN users u ON t.user_uid = u.bandbbs_uid
			ORDER BY t.created_at DESC
		`)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []AdminTicketItem
		for rows.Next() {
			var it AdminTicketItem
			if err := rows.Scan(&it.ID, &it.UserUID, &it.Username, &it.Kind, &it.Subject, &it.Message, &it.Status, &it.Resolution, &it.CreatedAt, &it.UpdatedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []AdminTicketItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleAdminReplyTicket implements POST /admin/api/tickets/:id (Spec §14.6)
func HandleAdminReplyTicket(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		ticketID := int64(httputil.ParseInt(parts[len(parts)-1], 0))
		if ticketID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "工单ID无效")
			return
		}

		var req struct {
			Message    string `json:"message"`
			Status     string `json:"status"`
			Resolution string `json:"resolution"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}
		if req.Status != "" && req.Status != "open" && req.Status != "in_progress" && req.Status != "resolved" && req.Status != "closed" {
			httputil.Error(w, http.StatusBadRequest, "invalid_status", "不支持的工单状态")
			return
		}
		if len([]rune(req.Message)) > 10000 || len([]rune(req.Resolution)) > 10000 {
			httputil.Error(w, http.StatusBadRequest, "content_too_long", "工单内容过长")
			return
		}

		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			if req.Message != "" {
				_, err := tx.Exec(r.Context(), `
					INSERT INTO feedback_replies (ticket_id, author_uid, message, is_admin)
					VALUES ($1, $2, $3, true)
				`, ticketID, adminUID, req.Message)
				if err != nil {
					return err
				}
			}

			if req.Status != "" {
				var previous string
				if err := tx.QueryRow(r.Context(), `SELECT status FROM feedback_tickets WHERE id = $1 FOR UPDATE`, ticketID).Scan(&previous); err != nil {
					return err
				}
				_, err := tx.Exec(r.Context(), `
					UPDATE feedback_tickets 
					SET status = $1, resolution = COALESCE(NULLIF($2, ''), resolution), updated_at = now()
					WHERE id = $3
				`, req.Status, req.Resolution, ticketID)
				if err != nil {
					return err
				}
				if previous != req.Status {
					_, err = tx.Exec(r.Context(), `INSERT INTO feedback_status_history (ticket_id, actor_uid, from_status, to_status) VALUES ($1, $2, $3, $4)`, ticketID, adminUID, previous, req.Status)
					if err != nil {
						return err
					}
				}
			}

			// Notify user
			var userUID int
			var subject string
			_ = tx.QueryRow(r.Context(), `SELECT user_uid, subject FROM feedback_tickets WHERE id = $1`, ticketID).Scan(&userUID, &subject)
			if userUID > 0 && req.Message != "" {
				_, _ = tx.Exec(r.Context(), `
					INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data)
					VALUES ($1, 'system', 'feedback.replied', '工单获得新回复', $2, $3, '{"kind":"feedback"}'::jsonb)
				`, userUID, fmt.Sprintf("你的工单《%s》已收到客服回复：%s", subject, req.Message), fmt.Sprintf("%d", ticketID))
			}
			return nil
		})

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "success"})
	}
}
