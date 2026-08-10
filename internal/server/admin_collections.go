package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminCollectionDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminCollection(r.Context(), r.PathValue("collection"))
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_collection_detail", map[string]any{"Title": detail.Collection.LatestRevisionName, "Detail": detail, "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminCollectionDraft(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	memberText := strings.NewReplacer(",", " ", "\r", " ", "\n", " ").Replace(r.FormValue("resource_ids"))
	revision, err := a.store.AdminUpdateCollectionMetadata(r.Context(), r.PathValue("collection"), store.AdminCollectionMetadataInput{
		Name: r.FormValue("name"), Summary: r.FormValue("summary"), Enabled: r.FormValue("enabled") == "on",
		RepresentativeResourceID: r.FormValue("representative_resource_id"), ResourceIDs: strings.Fields(memberText), CreatedBy: actor.UserID,
	})
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "collection.draft", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "collection.draft", "success", a.clientIP(r), r.UserAgent(), "collection="+r.PathValue("collection")+" revision="+revision.ID)
	http.Redirect(w, r, "/admin/collections/"+r.PathValue("collection")+"?action=drafted", http.StatusFound)
}
