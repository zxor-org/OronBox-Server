package community

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type MsgItem struct {
	ID        int64           `json:"id"`
	Kind      string          `json:"kind"`
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Ref       string          `json:"ref"`
	ReadAt    string          `json:"read_at,omitempty"`
	CreatedAt string          `json:"created_at"`
}

// HandleGetMessages implements GET /api/v1/messages (Spec §9.1)
func HandleGetMessages(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		page := httputil.ParseInt(r.URL.Query().Get("page"), 1)
		if page < 1 {
			page = 1
		}
		perPage := httputil.ParseInt(r.URL.Query().Get("per_page"), 0)
		if perPage <= 0 {
			perPage = httputil.ParseInt(r.URL.Query().Get("size"), 20)
		}
		if perPage <= 0 || perPage > 100 {
			perPage = 20
		}
		offset := (page - 1) * perPage

		unreadOnly := r.URL.Query().Get("unread_only") == "true" || r.URL.Query().Get("unread_only") == "1"

		query := `
			SELECT id, kind, event, data, COALESCE(title, ''), COALESCE(body, ''), COALESCE(ref, ''), 
			       COALESCE(to_char(read_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), ''), 
			       to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM user_messages
			WHERE user_uid = $1 AND (expires_at IS NULL OR expires_at > now())
		`
		var args []any
		args = append(args, userUID)

		if unreadOnly {
			query += ` AND read_at IS NULL`
		}

		idx1 := len(args) + 1
		idx2 := len(args) + 2
		query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, idx1, idx2)
		args = append(args, perPage, offset)

		rows, err := s.Pool.Query(r.Context(), query, args...)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var list []MsgItem
		for rows.Next() {
			var m MsgItem
			if err := rows.Scan(&m.ID, &m.Kind, &m.Event, &m.Data, &m.Title, &m.Body, &m.Ref, &m.ReadAt, &m.CreatedAt); err == nil {
				list = append(list, m)
			}
		}
		if list == nil {
			list = []MsgItem{}
		}

		var unread int
		_ = s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM user_messages WHERE user_uid = $1 AND read_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, userUID).Scan(&unread)
		httputil.JSON(w, http.StatusOK, map[string]any{"items": list, "unread": unread, "page": page, "size": perPage})
	}
}

// HandleMarkMessageRead implements PUT /api/v1/messages/:id/read (Spec §9.2)
func HandleMarkMessageRead(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		idStr := r.PathValue("id")
		if idStr == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "messages" && idx+1 < len(parts) {
					idStr = parts[idx+1]
					break
				}
			}
		}

		msgID := httputil.ParseInt(idStr, 0)
		if msgID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "消息ID无效")
			return
		}

		result, err := s.Pool.Exec(r.Context(), `
			UPDATE user_messages SET read_at = now() WHERE id = $1 AND user_uid = $2
		`, msgID, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "message_not_found", "消息不存在或无权访问")
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "read"})
	}
}

// HandleClearMessages marks all messages for the authenticated user as read.
func HandleClearMessages(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		if _, err := s.Pool.Exec(r.Context(), `UPDATE user_messages SET read_at = now() WHERE user_uid = $1 AND read_at IS NULL`, userUID); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"status": "cleared"})
	}
}

// HandleGetAnnouncements exposes announcement events through the same message
// table so clients do not need a second storage contract.
func HandleGetAnnouncements(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		rows, err := s.Pool.Query(r.Context(), `
			SELECT id, kind, event, data, COALESCE(title, ''), COALESCE(body, ''), COALESCE(ref, ''),
			       COALESCE(to_char(read_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), ''),
			       to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM user_messages WHERE user_uid = $1 AND event LIKE 'announcement.%' AND read_at IS NULL AND (expires_at IS NULL OR expires_at > now())
			ORDER BY created_at DESC LIMIT 100
		`, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()
		items := make([]MsgItem, 0)
		for rows.Next() {
			var item MsgItem
			if rows.Scan(&item.ID, &item.Kind, &item.Event, &item.Data, &item.Title, &item.Body, &item.Ref, &item.ReadAt, &item.CreatedAt) == nil {
				items = append(items, item)
			}
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"announcements": items})
	}
}

func HandleReadAnnouncements(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		if _, err := s.Pool.Exec(r.Context(), `UPDATE user_messages SET read_at = now() WHERE user_uid = $1 AND event LIKE 'announcement.%' AND read_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, userUID); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"status": "read"})
	}
}
