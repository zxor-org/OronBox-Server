package community

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type FeedbackTicketItem struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type FeedbackReplyItem struct {
	ID         int64  `json:"id"`
	AuthorUID  int    `json:"author_uid"`
	AuthorRole string `json:"author_role"`
	Message    string `json:"message"`
	CreatedAt  string `json:"created_at"`
}

type FeedbackDetailResponse struct {
	ID         int64               `json:"id"`
	Kind       string              `json:"kind"`
	Subject    string              `json:"subject"`
	Message    string              `json:"message"`
	Status     string              `json:"status"`
	Resolution string              `json:"resolution"`
	CreatedAt  string              `json:"created_at"`
	UpdatedAt  string              `json:"updated_at"`
	Replies    []FeedbackReplyItem `json:"replies"`
}

// HandleGetMyFeedback implements GET /api/v1/feedback (Spec §11.1)
func HandleGetMyFeedback(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT id, kind, subject, message, status,
			       to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM feedback_tickets
			WHERE user_uid = $1
			ORDER BY created_at DESC
		`, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []FeedbackTicketItem
		for rows.Next() {
			var it FeedbackTicketItem
			if err := rows.Scan(&it.ID, &it.Kind, &it.Subject, &it.Message, &it.Status, &it.CreatedAt, &it.UpdatedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []FeedbackTicketItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleCreateFeedback implements POST /api/v1/feedback (Spec §11.2)
func HandleCreateFeedback(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var req struct {
			Kind         string `json:"kind"`
			Subject      string `json:"subject"`
			Message      string `json:"message"`
			TargetSource string `json:"target_source"`
			TargetID     string `json:"target_id"`
			TargetURL    string `json:"target_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}
		if req.Subject == "" || req.Message == "" {
			httputil.Error(w, http.StatusBadRequest, "missing_fields", "缺少标题或描述正文")
			return
		}
		if req.Kind == "" {
			req.Kind = "defect"
		}

		var id int64
		err = s.Pool.QueryRow(r.Context(), `
			INSERT INTO feedback_tickets (user_uid, kind, subject, message, target_source, target_id, target_url, status)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'open')
			RETURNING id
		`, userUID, req.Kind, req.Subject, req.Message, req.TargetSource, req.TargetID, req.TargetURL).Scan(&id)

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"id":      id,
			"status":  "open",
			"subject": req.Subject,
			"kind":    req.Kind,
		})
	}
}

// HandleGetFeedbackDetail implements GET /api/v1/feedback/:ticket_id (Spec §11.3)
func HandleGetFeedbackDetail(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		ticketID := int64(httputil.ParseInt(parts[len(parts)-1], 0))
		if ticketID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "工单ID无效")
			return
		}

		var detail FeedbackDetailResponse
		err = s.Pool.QueryRow(r.Context(), `
			SELECT id, kind, subject, message, status, resolution,
			       to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM feedback_tickets
			WHERE id = $1 AND user_uid = $2
		`, ticketID, userUID).Scan(&detail.ID, &detail.Kind, &detail.Subject, &detail.Message, &detail.Status, &detail.Resolution, &detail.CreatedAt, &detail.UpdatedAt)

		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "not_found", "工单不存在")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT r.id, r.author_uid, CASE WHEN r.is_admin THEN 'admin' ELSE 'user' END,
			       r.message, to_char(r.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM feedback_replies r
			WHERE r.ticket_id = $1
			ORDER BY r.created_at ASC
		`, ticketID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var rep FeedbackReplyItem
				if err := rows.Scan(&rep.ID, &rep.AuthorUID, &rep.AuthorRole, &rep.Message, &rep.CreatedAt); err == nil {
					detail.Replies = append(detail.Replies, rep)
				}
			}
		}
		if detail.Replies == nil {
			detail.Replies = []FeedbackReplyItem{}
		}

		httputil.JSON(w, http.StatusOK, detail)
	}
}

// HandlePostFeedbackReply implements POST /api/v1/feedback/:ticket_id/replies (Spec §11.4)
func HandlePostFeedbackReply(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		var ticketID int64
		for idx, p := range parts {
			if p == "feedback" && idx+1 < len(parts) {
				ticketID = int64(httputil.ParseInt(parts[idx+1], 0))
				break
			}
		}
		if ticketID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "工单ID无效")
			return
		}

		var req struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Message == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "缺少回复内容")
			return
		}

		var exists bool
		_ = s.Pool.QueryRow(r.Context(), `SELECT true FROM feedback_tickets WHERE id = $1 AND user_uid = $2`, ticketID, userUID).Scan(&exists)
		if !exists {
			httputil.Error(w, http.StatusNotFound, "ticket_not_found", "工单不存在")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			INSERT INTO feedback_replies (ticket_id, author_uid, message, is_admin)
			VALUES ($1, $2, $3, false)
		`, ticketID, userUID, req.Message)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		_, _ = s.Pool.Exec(r.Context(), `UPDATE feedback_tickets SET updated_at = now() WHERE id = $1`, ticketID)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "replied"})
	}
}
