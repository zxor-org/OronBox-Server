package server

import (
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminHomeResourceOptions(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminResources(r.Context(), store.AdminResourceQuery{Search: r.URL.Query().Get("q"), Moderation: "visible", CurrentRevisionState: "approved", Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: 25, Sort: "updated_desc"})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("resource_options_failed", err.Error()))
		return
	}
	items := make([]map[string]string, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]string{"id": item.ID, "name": item.Name, "slug": item.Slug, "owner": item.Owner})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page.Page, "total": page.Total, "total_pages": page.TotalPages})
}
