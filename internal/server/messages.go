package server

import (
	"fmt"
	"net/http"
	"strings"
)

func (a *App) handleMessages(w http.ResponseWriter, r *http.Request) {
	items, unread, err := a.store.UserMessages(r.Context(), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("messages_read_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": items, "unread": unread})
}

func (a *App) handleReadMessage(w http.ResponseWriter, r *http.Request) {
	if err := a.store.ReadUserMessage(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("message_read_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleClearMessages(w http.ResponseWriter, r *http.Request) {
	if err := a.store.ClearUserMessages(r.Context(), currentUser(r).ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("messages_clear_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleAnnouncements(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.UnreadAnnouncements(r.Context(), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("announcements_read_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"announcements": items})
}

func (a *App) handleReadAnnouncements(w http.ResponseWriter, r *http.Request) {
	if err := a.store.ReadAnnouncements(r.Context(), currentUser(r).ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("announcement_read_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleAdminUserMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	if title == "" || body == "" {
		http.Error(w, "title and body are required", http.StatusBadRequest)
		return
	}
	count, err := a.store.CreateAdminMessages(r.Context(), r.Form["user"], title, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	_ = a.store.RecordAudit(r.Context(), actor, "message.send", "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("recipients=%d title=%s", count, title))
	http.Redirect(w, r, "/admin/users?action=message_sent", http.StatusFound)
}
