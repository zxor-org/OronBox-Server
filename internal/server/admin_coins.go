package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminCoinsPage(w http.ResponseWriter, r *http.Request) {
	stats, err := a.store.AdminCoinStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page, perPage := positiveInt(r.URL.Query().Get("page"), 1), positiveInt(r.URL.Query().Get("per_page"), 25)
	if perPage > 100 {
		perPage = 100
	}
	from, to := adminTimeRange(r.URL.Query())
	ledger, err := a.store.AdminCoinLedgerPage(r.Context(), store.AdminCoinQuery{Search: r.URL.Query().Get("q"), User: r.URL.Query().Get("user"), Kind: r.URL.Query().Get("kind"), ReferenceType: r.URL.Query().Get("reference_type"), Sort: r.URL.Query().Get("sort"), From: from, To: to, Page: page, PerPage: perPage})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	users, err := a.store.AdminCoinUserOptions(r.Context(), r.URL.Query().Get("user_search"), 30)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_coins", map[string]any{"Title": "硬币管理", "Stats": stats, "Ledger": ledger.Items, "Page": ledger, "Query": ledger.Query, "Users": users, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/coins", r.URL.Query(), ledger.Page, ledger.PerPage, ledger.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) applyAdminCoinUserAction(ctx context.Context, userID, action string, deltaUnits int64, reason string, actorID string) (any, error) {
	switch action {
	case "adjust":
		return a.store.AdminAdjustCoins(ctx, userID, deltaUnits, reason, actorID)
	case "freeze":
		return map[string]any{"ok": true}, a.store.AdminSetCoinFreeze(ctx, userID, true, reason)
	case "unfreeze":
		return map[string]any{"ok": true}, a.store.AdminSetCoinFreeze(ctx, userID, false, reason)
	default:
		return nil, fmt.Errorf("unknown coin action")
	}
}

func (a *App) handleAdminCoinUserForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	userID, action, reason := strings.TrimSpace(r.FormValue("user_id")), r.FormValue("action"), strings.TrimSpace(r.FormValue("reason"))
	var delta int64
	var err error
	if action == "adjust" {
		delta, err = strconv.ParseInt(r.FormValue("delta_units"), 10, 64)
	}
	if err == nil {
		_, err = a.applyAdminCoinUserAction(r.Context(), userID, action, delta, reason, actor.UserID)
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "coins."+action, "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "coins."+action, "success", a.clientIP(r), r.UserAgent(), "user="+userID+" reason="+reason)
	http.Redirect(w, r, "/admin/coins?action="+action, http.StatusFound)
}

func (a *App) handleAdminCoinInvalidateForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	err := a.store.AdminInvalidateCoinVote(r.Context(), r.FormValue("resource_id"), r.FormValue("user_id"), r.FormValue("reason"), actor.UserID)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "coins.invalidate", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "coins.invalidate", "success", a.clientIP(r), r.UserAgent(), "resource="+r.FormValue("resource_id")+" user="+r.FormValue("user_id"))
	http.Redirect(w, r, "/admin/coins?action=invalidated", http.StatusFound)
}
