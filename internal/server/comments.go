package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleListComments(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Authorization")
	if _, err := a.creator.PublicResource(r.Context(), r.PathValue("id")); err != nil {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("resource_not_found", "resource was not found"))
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	cursor, err := store.DecodeCommunityCursor(r.URL.Query().Get("before"))
	if err != nil {
		// Existing legacy callers send RFC3339 timestamps. New clients use the
		// timestamp+UUID cursor to avoid skipping records with equal timestamps.
		if at, e := time.Parse(time.RFC3339Nano, r.URL.Query().Get("before")); e == nil {
			cursor = store.CommunityCursor{At: at, ID: uuid.Nil.String()}
			err = nil
		}
	}
	if err != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_cursor", "分页游标无效"))
		return
	}
	page, err := a.store.CommentRoots(r.Context(), r.PathValue("id"), cursor, size, a.optionalUserID(r))
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("comments_read_failed", "读取评论失败"))
		return
	}
	httpx.WriteJSON(w, 200, page)
}

func (a *App) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	if request.ParentID != "" {
		if _, err := uuid.Parse(request.ParentID); err != nil {
			httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_parent", "回复目标无效"))
			return
		}
	}
	request.Body = strings.TrimSpace(request.Body)
	if size := len([]rune(request.Body)); size < 1 || size > 2000 {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_comment", "comment must contain 1 to 2000 characters"))
		return
	}
	state := "visible"
	var verdict moderation.Verdict
	a.runtime.mu.RLock()
	moderator := a.moderation
	a.runtime.mu.RUnlock()
	if moderator != nil && moderator.Enabled() {
		prompt, err := a.store.Setting(r.Context(), "moderation.prompt", moderation.DefaultPrompt)
		if err != nil {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("moderation_settings_failed", err.Error()))
			return
		}
		verdict, err = moderator.Review(r.Context(), prompt, request.Body)
		if err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, httpx.ErrorBody("moderation_unavailable", err.Error()))
			return
		}
		if verdict.Action != "pass" {
			state = "hidden"
		}
	}
	var moderationRecord *store.CommentModerationRecord
	if verdict.Action != "" {
		moderationRecord = &store.CommentModerationRecord{Provider: verdict.Provider, Model: verdict.Model, Action: verdict.Action, Categories: verdict.Categories, Reason: verdict.Reason, Raw: verdict.Raw}
	}
	comment, err := a.store.CreateModeratedComment(r.Context(), r.PathValue("id"), currentUser(r).ID, strings.TrimSpace(request.ParentID), request.Body, state, moderationRecord)
	if errors.Is(err, store.ErrCommentTooFast) || errors.Is(err, store.ErrCommentQuota) {
		httpx.WriteJSON(w, http.StatusTooManyRequests, httpx.ErrorBody("comment_rate_limited", err.Error()))
		return
	}
	if errors.Is(err, store.ErrCommentNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("comment_target_not_found", err.Error()))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("comment_create_failed", err.Error()))
		return
	}
	if verdict.Action == "block" {
		httpx.WriteJSON(w, http.StatusUnprocessableEntity, httpx.ErrorBody("comment_blocked", "评论未通过社区规范"))
		return
	}
	comment.ModerationAction = verdict.Action
	httpx.WriteJSON(w, http.StatusCreated, comment)
}

func (a *App) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var err error
	if user.Role == "admin" {
		err = a.store.AdminDeleteComment(r.Context(), r.PathValue("id"))
	} else {
		err = a.store.SoftDeleteComment(r.Context(), r.PathValue("id"), user.ID)
	}
	if errors.Is(err, store.ErrCommentNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("comment_not_found", err.Error()))
	} else if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("comment_delete_failed", err.Error()))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) handleCommentThread(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Authorization")
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		httpx.WriteJSON(w, 404, httpx.ErrorBody("comment_not_found", "评论不存在"))
		return
	}
	before, e1 := store.DecodeCommunityCursor(r.URL.Query().Get("before"))
	after, e2 := store.DecodeCommunityCursor(r.URL.Query().Get("after"))
	if e1 != nil || e2 != nil || (!before.At.IsZero() && !after.At.IsZero()) {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_cursor", "分页游标无效"))
		return
	}
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	page, err := a.store.CommentThread(r.Context(), id, before, after, size, a.optionalUserID(r))
	if errors.Is(err, store.ErrCommentNotFound) {
		httpx.WriteJSON(w, 404, httpx.ErrorBody("comment_not_found", "评论不存在或不可见"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("comments_read_failed", "读取评论失败"))
		return
	}
	httpx.WriteJSON(w, 200, page)
}
