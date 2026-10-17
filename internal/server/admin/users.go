package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIUsers(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminUsers(r.Context(), store.AdminUserQuery{
		Role: r.URL.Query().Get("role"), Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("users_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "username": item.Username, "avatar_url": item.AvatarURL,
			"bandbbs_user_id": item.BandBBSUserID, "role": item.Role,
			"resource_count": item.ResourceCount, "ticket_count": item.TicketCount,
			"banned": item.BannedAt != nil, "banned_at": item.BannedAt,
			"frozen": item.CreatorFrozenAt != nil, "creator_frozen_at": item.CreatorFrozenAt,
			"ban_reason": item.BanReason, "created_at": item.CreatedAt,
			"updated_at": item.UpdatedAt, "last_seen_at": item.LastSeenAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "users_failed")
}

func (a *Handler) HandleAdminAPICoinLedger(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminCoinLedgerPage(r.Context(), store.AdminCoinQuery{
		Search: r.URL.Query().Get("q"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("coins_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "user_id": item.UserID, "kind": item.Kind, "delta_units": item.DeltaUnits,
			"reference_type": item.ReferenceType, "reference_id": item.ReferenceID, "note": item.Note,
			"username": item.Username, "created_at": item.CreatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "coins_failed")
}

func (a *Handler) HandleAdminAPIUserState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	actor := a.actor(r)
	if _, err := a.store.AdminManageUser(r.Context(), r.PathValue("user"), body.Action, body.Reason, body.Role, actor); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("user_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIUserDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminUserDetail(r.Context(), r.PathValue("user"), store.AdminUserDetailQuery{})
	if err != nil {
		if errors.Is(err, store.ErrAdminUserNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", err.Error()))
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("user_detail_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *Handler) HandleAdminAPIUserSessions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		All       bool   `json:"all"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	var err error
	if body.All {
		_, err = a.store.AdminRevokeAllUserSessions(r.Context(), r.PathValue("user"))
	} else {
		err = a.store.AdminRevokeUserSession(r.Context(), r.PathValue("user"), body.SessionID)
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("user_failed", err.Error()))
		return
	}
	a.audit(r, "user.sessions.revoke", "success", "user="+r.PathValue("user"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
