package server

import (
	"net/http"
	"strconv"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleMessages(w http.ResponseWriter, r *http.Request) {
	cursor, err := store.DecodeCommunityCursor(r.URL.Query().Get("before"))
	if err != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_cursor", "分页游标无效"))
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	page, err := a.store.UserMessagePage(r.Context(), currentUser(r).ID, cursor, size, r.URL.Query().Get("unread_only") == "true" || r.URL.Query().Get("unread_only") == "1")
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("messages_read_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (a *App) handleReadMessage(w http.ResponseWriter, r *http.Request) {
	if err := a.store.ReadUserMessage(r.Context(), currentUser(r).ID, r.PathValue("id")); err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("message_read_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleClearMessages(w http.ResponseWriter, r *http.Request) {
	if err := a.store.ClearUserMessages(r.Context(), currentUser(r).ID); err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("messages_clear_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleAnnouncements(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.UnreadAnnouncements(r.Context(), currentUser(r).ID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("announcements_read_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"announcements": items})
}

func (a *App) handleReadAnnouncements(w http.ResponseWriter, r *http.Request) {
	if err := a.store.ReadAnnouncements(r.Context(), currentUser(r).ID); err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("announcement_read_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
