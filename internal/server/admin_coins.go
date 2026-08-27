package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

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
