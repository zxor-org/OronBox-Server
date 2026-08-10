package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminPublications(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminPublications(r.Context(), store.AdminPublicationQuery{
		Search: r.URL.Query().Get("q"), Target: r.URL.Query().Get("target"), State: r.URL.Query().Get("state"),
		Resource: r.URL.Query().Get("resource"), Owner: r.URL.Query().Get("owner"), Sort: r.URL.Query().Get("sort"),
		Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_publications", map[string]any{"Title": "发布任务", "Page": page, "Items": page.Items, "Query": page.Query, "Pager": web.NewPagination("/admin/publications", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action"), "Retried": r.URL.Query().Get("retried")})
}

func (a *App) handleAdminPublicationBatchRetry(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	query := store.AdminPublicationQuery{
		Search: r.FormValue("q"), Target: r.FormValue("target"), State: "failed",
		Resource: r.FormValue("resource"), Owner: r.FormValue("owner"), Sort: r.FormValue("sort"),
	}
	actor := currentAdmin(r)
	result, err := a.store.AdminRetryFailedPublications(r.Context(), query)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "publication.batch_retry", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		status := http.StatusInternalServerError
		if errors.Is(err, store.ErrAdminPublicationConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "publication.batch_retry", "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("matched=%d retried=%d target=%s resource=%s owner=%s q=%s", result.Matched, result.Retried, query.Target, query.Resource, query.Owner, query.Search))
	redirect := url.Values{"state": {"failed"}, "action": {"batch_retried"}, "matched": {fmt.Sprint(result.Matched)}, "retried": {fmt.Sprint(result.Retried)}}
	for _, key := range []string{"q", "target", "resource", "owner", "sort", "per_page"} {
		if value := r.FormValue(key); value != "" {
			redirect.Set(key, value)
		}
	}
	http.Redirect(w, r, "/admin/publications?"+redirect.Encode(), http.StatusFound)
}

func (a *App) handleAdminPublication(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.AdminPublication(r.Context(), r.PathValue("publication"))
	if errors.Is(err, store.ErrAdminPublicationNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_publication_detail", map[string]any{"Title": item.RevisionName, "Item": item, "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminPublicationAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	item, err := a.store.AdminManagePublication(r.Context(), r.PathValue("publication"), r.FormValue("action"))
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "publication."+r.FormValue("action"), "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "publication."+r.FormValue("action"), "success", a.clientIP(r), r.UserAgent(), "publication="+item.ID)
	http.Redirect(w, r, "/admin/publications/"+item.ID+"?action="+r.FormValue("action"), http.StatusFound)
}
