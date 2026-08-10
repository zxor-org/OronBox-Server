package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/moderation"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

const defaultModerationPrompt = `你是 OronBox 社区评论审核器。审核分类仅限 porn、politics、abuse、spam、illegal。明确违规返回 block，无法确定返回 review，正常内容返回 pass。只返回 JSON 对象，格式为 {"action":"pass|review|block","categories":[],"reason":"简短理由"}。`

func (a *App) handleListComments(w http.ResponseWriter, r *http.Request) {
	if _, err := a.creator.PublicResource(r.Context(), r.PathValue("id")); err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("resource_not_found", "resource was not found"))
		return
	}
	viewerID := ""
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) >= 8 && strings.EqualFold(header[:7], "Bearer ") {
		if user, err := a.store.UserByAccessToken(r.Context(), authcore.HashToken(strings.TrimSpace(header[7:]), a.cfg.SessionSecret)); err == nil {
			viewerID = user.ID
		}
	}
	before := time.Now().UTC().Add(time.Second)
	if value := strings.TrimSpace(r.URL.Query().Get("before")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody("invalid_before", "before must be RFC3339"))
			return
		}
		before = parsed
	}
	comments, err := a.store.ListComments(r.Context(), r.PathValue("id"), viewerID, before, 20)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("comments_read_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"comments": comments})
}

func (a *App) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	if size := len([]rune(request.Body)); size < 1 || size > 2000 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_comment", "comment must contain 1 to 2000 characters"))
		return
	}
	state := "visible"
	var verdict moderation.Verdict
	if a.moderation != nil && a.moderation.Enabled() {
		prompt, err := a.store.Setting(r.Context(), "moderation.prompt", defaultModerationPrompt)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("moderation_settings_failed", err.Error()))
			return
		}
		verdict, err = a.moderation.Review(r.Context(), prompt, request.Body)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody("moderation_unavailable", "moderation service is unavailable"))
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
		writeJSON(w, http.StatusTooManyRequests, errorBody("comment_rate_limited", err.Error()))
		return
	}
	if errors.Is(err, store.ErrCommentNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("comment_target_not_found", err.Error()))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("comment_create_failed", err.Error()))
		return
	}
	if verdict.Action == "block" {
		writeJSON(w, http.StatusUnprocessableEntity, errorBody("comment_blocked", verdict.Reason))
		return
	}
	comment.ModerationAction = verdict.Action
	writeJSON(w, http.StatusCreated, comment)
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
		writeJSON(w, http.StatusNotFound, errorBody("comment_not_found", err.Error()))
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("comment_delete_failed", err.Error()))
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *App) handleAdminComments(w http.ResponseWriter, r *http.Request) {
	page := positiveInt(r.URL.Query().Get("page"), 1)
	perPage := positiveInt(r.URL.Query().Get("per_page"), 25)
	if perPage > 100 {
		perPage = 100
	}
	result, err := a.store.AdminComments(r.Context(), store.AdminCommentQuery{Search: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Resource: r.URL.Query().Get("resource"), User: r.URL.Query().Get("user"), Sort: r.URL.Query().Get("sort"), Page: page, PerPage: perPage})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	prompt, err := a.store.Setting(r.Context(), "moderation.prompt", defaultModerationPrompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_comments", map[string]any{
		"Title": "评论管理", "Items": result.Items, "Total": result.Total, "Page": result.Page, "PerPage": result.PerPage, "Query": result.Query,
		"Pager": web.NewPagination("/admin/comments", r.URL.Query(), result.Page, result.PerPage, result.Total), "Prompt": prompt,
	})
}

func (a *App) handleAdminCommentDecision(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimSpace(r.FormValue("action"))
	err := a.store.AdminModerateComment(r.Context(), r.PathValue("comment"), action)
	actor := currentAdmin(r)
	result := "success"
	if err != nil {
		result = "failure"
	}
	_ = a.store.RecordAudit(r.Context(), actor, "comment."+action, result, a.clientIP(r), r.UserAgent(), "comment="+r.PathValue("comment"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/admin/comments", http.StatusFound)
}

func (a *App) handleAdminModerationPrompt(w http.ResponseWriter, r *http.Request) {
	prompt := strings.TrimSpace(r.FormValue("prompt"))
	if prompt == "" {
		prompt = defaultModerationPrompt
	}
	if err := a.store.SetSetting(r.Context(), "moderation.prompt", prompt); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "moderation.prompt", "success", a.clientIP(r), r.UserAgent(), "")
	http.Redirect(w, r, "/admin/comments", http.StatusFound)
}

func (a *App) handleAdminModerationTest(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.FormValue("text"))
	prompt, err := a.store.Setting(r.Context(), "moderation.prompt", defaultModerationPrompt)
	var result string
	if err == nil && a.moderation != nil && a.moderation.Enabled() {
		verdict, reviewErr := a.moderation.Review(r.Context(), prompt, text)
		err = reviewErr
		if err == nil {
			raw, _ := json.MarshalIndent(verdict.Raw, "", "  ")
			result = string(raw)
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	commentPage, listErr := a.store.AdminComments(r.Context(), store.AdminCommentQuery{Page: page, PerPage: 25})
	if listErr != nil {
		http.Error(w, listErr.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_comments", map[string]any{"Title": "评论管理", "Items": commentPage.Items, "Total": commentPage.Total, "Page": page, "Prompt": prompt, "TestText": text, "TestResult": result})
}
