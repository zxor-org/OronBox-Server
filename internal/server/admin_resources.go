package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminResourceState(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	resourceID := r.PathValue("resource")
	action := strings.TrimSpace(r.FormValue("action"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	actor := currentAdmin(r)
	result, err := a.store.AdminManageResource(r.Context(), resourceID, action, reason, actor)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource."+action, "failure", a.clientIP(r), r.UserAgent(), "resource="+resourceID+" error="+err.Error())
		status := http.StatusConflict
		if errors.Is(err, store.ErrAdminResourceNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource."+action, "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("resource=%s slug=%s previous_moderation=%s moderation=%s reason=%s deleted=%t", result.ID, result.Slug, result.PreviousModeration, result.ModerationState, result.Reason, result.Deleted))
	observability.From(r.Context()).With("component", "admin").Info(
		"admin resource state changed",
		"resource_id", resourceID,
		"action", action,
		"admin_user", actor.Username,
		"previous_moderation", result.PreviousModeration,
		"moderation", result.ModerationState,
		"reason", result.Reason,
	)
	if result.Deleted {
		http.Redirect(w, r, "/admin/resources?action=deleted", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/resources/"+resourceID+"?action="+action, http.StatusFound)
}
