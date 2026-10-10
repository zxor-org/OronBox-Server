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
				_, err := tx.Exec(r.Context(), `
					UPDATE feedback_tickets 
					SET status = $1, resolution = COALESCE(NULLIF($2, ''), resolution), updated_at = now()
					WHERE id = $3
				`, req.Status, req.Resolution, ticketID)
				if err != nil {
					return err
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
