package server

import (
	"net/http"
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
