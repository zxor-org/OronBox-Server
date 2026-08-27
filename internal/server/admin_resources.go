package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminResources(w http.ResponseWriter, r *http.Request) {
	query := store.AdminResourceQuery{
		Search:            r.URL.Query().Get("q"),
		Owner:             r.URL.Query().Get("owner"),
		Kind:              r.URL.Query().Get("kind"),
		Moderation:        r.URL.Query().Get("moderation"),
		RevisionState:     r.URL.Query().Get("revision_state"),
		ReviewState:       r.URL.Query().Get("review_state"),
		PublicationTarget: r.URL.Query().Get("target"),
		PublicationState:  r.URL.Query().Get("publication_state"),
		Sort:              r.URL.Query().Get("sort"),
		Page:              positiveInt(r.URL.Query().Get("page"), 1),
		PerPage:           positiveInt(r.URL.Query().Get("per_page"), 25),
	}
	page, err := a.store.AdminResources(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_resources", map[string]any{
		"Title":  "资源",
		"Page":   page,
		"Pager":  web.NewPagination("/admin/resources", r.URL.Query(), page.Page, page.PerPage, page.Total),
		"Items":  page.Items,
		"Query":  page.Query,
		"Action": r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminResource(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminResource(r.Context(), r.PathValue("resource"))
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_resource_detail", map[string]any{
		"Title":        detail.Resource.Name,
		"Item":         detail.Resource,
		"Detail":       detail,
		"Publications": detail.Publications,
		"Artifacts":    detail.Artifacts,
		"Media":        detail.Media,
		"Snapshot":     detail.Snapshot,
		"Action":       r.URL.Query().Get("action"),
	})
}

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
