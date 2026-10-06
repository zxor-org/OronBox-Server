package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func (a *application) releases(w http.ResponseWriter, r *http.Request) {
	lang := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("lang")))
	if a.db == nil {
		jsonError(w, http.StatusNotFound, "release_not_found", "no release cache available", nil)
		return
	}
	var version, minimum, zh, en string
	var published time.Time
	query := `SELECT version,minimum_version,COALESCE(notes_zh,''),COALESCE(notes_en,''),published_at FROM app_releases ORDER BY published_at DESC LIMIT 1`
	if err := a.db.Pool.QueryRow(r.Context(), query).Scan(&version, &minimum, &zh, &en, &published); err != nil {
		jsonError(w, http.StatusNotFound, "release_not_found", "no release cache available", nil)
		return
	}
	notes := zh
	if lang == "en" && strings.TrimSpace(en) != "" {
		notes = en
	}
	jsonResponse(w, http.StatusOK, map[string]any{"latest_version": version, "minimum_version": minimum, "release_notes": notes, "published_at": published})
}

func (a *application) notices(w http.ResponseWriter, r *http.Request) {
	if a.db == nil {
		jsonResponse(w, 200, map[string]any{"updated_at": time.Now().UTC(), "notices": []any{}})
		return
	}
	var raw []byte
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT value FROM server_settings WHERE key='notices'`).Scan(&raw); err != nil {
		jsonResponse(w, 200, map[string]any{"updated_at": time.Now().UTC(), "notices": []any{}})
		return
	}
	var notices any
	if json.Unmarshal(raw, &notices) != nil {
		notices = []any{}
	}
	jsonResponse(w, 200, map[string]any{"updated_at": time.Now().UTC(), "notices": notices})
}

func (a *application) feedbackItem(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 {
		jsonError(w, 404, "not_found", "feedback ticket not found", nil)
		return
	}
	ticketID := parts[3]
	if strings.HasSuffix(r.URL.Path, "/replies") && r.Method == http.MethodPost {
		var in struct {
			Message string `json:"message"`
		}
		if !readJSON(w, r, &in) || strings.TrimSpace(in.Message) == "" {
			jsonError(w, 400, "message_required", "message is required", nil)
			return
		}
		var owner string
		if err := a.db.Pool.QueryRow(r.Context(), `SELECT user_id FROM feedback_tickets WHERE id=$1`, ticketID).Scan(&owner); err != nil || owner != u.ID {
			jsonError(w, 404, "ticket_not_found", "feedback ticket not found", nil)
			return
		}
		var id string
		if err := a.db.Pool.QueryRow(r.Context(), `INSERT INTO feedback_replies(ticket_id,author_id,message,is_admin) VALUES($1,$2,$3,false) RETURNING id`, ticketID, u.ID, in.Message).Scan(&id); err != nil {
			jsonError(w, 500, "feedback_reply_failed", err.Error(), nil)
			return
		}
		_, _ = a.db.Pool.Exec(r.Context(), `UPDATE feedback_tickets SET updated_at=now() WHERE id=$1`, ticketID)
		jsonResponse(w, 201, map[string]any{"id": id, "ticket_id": ticketID, "message": in.Message})
		return
	}
	if r.Method != http.MethodGet {
		jsonError(w, 405, "method_not_allowed", "method not allowed", nil)
		return
	}
	var targetSource, title, status string
	var owner string
	var created, updated time.Time
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT user_id,COALESCE(target_source,''),title,status,created_at,updated_at FROM feedback_tickets WHERE id=$1`, ticketID).Scan(&owner, &targetSource, &title, &status, &created, &updated); err != nil || owner != u.ID {
		jsonError(w, 404, "ticket_not_found", "feedback ticket not found", nil)
		return
	}
	rows, err := a.db.Pool.Query(r.Context(), `SELECT id,author_id,message,created_at FROM feedback_replies WHERE ticket_id=$1 ORDER BY created_at`, ticketID)
	if err != nil {
		jsonError(w, 500, "feedback_replies_failed", err.Error(), nil)
		return
	}
	defer rows.Close()
	replies := []any{}
	for rows.Next() {
		var id, author, body string
		var at time.Time
		if rows.Scan(&id, &author, &body, &at) == nil {
			replies = append(replies, map[string]any{"id": id, "author_id": author, "message": body, "created_at": at})
		}
	}
	jsonResponse(w, 200, map[string]any{"ticket": map[string]any{"id": ticketID, "target_source": targetSource, "title": title, "status": status, "created_at": created, "updated_at": updated}, "replies": replies})
}
