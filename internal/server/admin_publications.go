package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

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
