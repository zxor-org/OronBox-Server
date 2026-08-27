package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

func (a *App) handleAdminAnnouncement(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	if title == "" || body == "" {
		http.Error(w, "title and body are required", http.StatusBadRequest)
		return
	}
	if err := a.store.CreateAnnouncement(r.Context(), currentAdmin(r).UserID, title, body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "announcement.publish", "success", a.clientIP(r), r.UserAgent(), title)
	http.Redirect(w, r, "/admin/announcements?action=published", http.StatusFound)
}

func (a *App) handleAdminDeleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("announcement")
	actor := currentAdmin(r)
	if err := a.store.DeleteAnnouncement(r.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "announcement.delete", "success", a.clientIP(r), r.UserAgent(), "announcement="+id)
	http.Redirect(w, r, "/admin/announcements?action=deleted", http.StatusFound)
}
