package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPICommentDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if strings.TrimSpace(body.Action) == "hide" && strings.TrimSpace(body.Note) == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "隐藏评论必须填写理由"))
		return
	}
	actor := a.actor(r)
	if err := a.store.AdminModerateComment(r.Context(), r.PathValue("comment"), body.Action, actor.UserID, body.Note); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("comment_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPICommentBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
		Note   string   `json:"note"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if body.Action == "hide" && strings.TrimSpace(body.Note) == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "批量隐藏必须填写理由"))
		return
	}
	actor := a.actor(r)
	if err := a.store.AdminModerateCommentsBatch(r.Context(), body.IDs, body.Action, actor.UserID, body.Note); err != nil {
		a.audit(r, "comment.bulk."+body.Action, "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("comment_failed", err.Error()))
		return
	}
	a.audit(r, "comment.bulk."+body.Action, "success", strings.Join(body.IDs, ","))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIComments(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminComments(r.Context(), store.AdminCommentQuery{
		Search:  r.URL.Query().Get("q"),
		State:   r.URL.Query().Get("state"),
		Page:    httpx.PositiveInt(r.URL.Query().Get("page"), 1),
		PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("comments_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id":      item.ID,
			"body":    item.Body,
			"user_id": item.UserID, "deleted": item.Deleted, "moderation_reason": item.ModerationReason, "ai_action": item.ModerationAction,
			"username":    item.Username,
			"resource_id": item.ResourceID,
			"state":       item.ModerationState,
			"created_at":  item.CreatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": page.Total, "page": page.Page, "per_page": page.PerPage,
	})
}
