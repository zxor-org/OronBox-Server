package admin

import (
	"encoding/json"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIPublications(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminPublications(r.Context(), store.AdminPublicationQuery{
		Search: r.URL.Query().Get("q"), Target: r.URL.Query().Get("target"), State: r.URL.Query().Get("state"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("publications_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "name": item.RevisionName, "target": item.Target, "state": item.State, "error": item.ErrorMessage,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "publications_failed")
}

func (a *Handler) HandleAdminAPIPublicationAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	actor := a.actor(r)
	if _, err := a.store.AdminManagePublication(r.Context(), r.PathValue("publication"), body.Action); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "publication."+body.Action, "failure", a.clientIP(r), r.UserAgent(), err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("publication_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "publication."+body.Action, "success", a.clientIP(r), r.UserAgent(), "publication="+r.PathValue("publication"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIPublicationsRetry(w http.ResponseWriter, r *http.Request) {
	result, err := a.store.AdminRetryFailedPublications(r.Context(), store.AdminPublicationQuery{
		Search: r.URL.Query().Get("q"), Target: r.URL.Query().Get("target"),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("publication_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}
