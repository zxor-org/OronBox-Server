package admin

import (
	"errors"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPICollections(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminCollections(r.Context(), store.AdminCollectionQuery{
		Search: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Page: httpx.PositiveInt(r.URL.Query().Get("page"), 1), PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("collections_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "name": item.LatestRevisionName, "owner": item.Owner, "owner_id": item.OwnerID,
			"slug": item.Slug, "kind": item.Kind, "platform": item.Platform,
			"state": item.LatestRevisionState, "member_count": item.MemberCount,
			"enabled": item.Enabled, "updated_at": item.UpdatedAt, "created_at": item.CreatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "collections_failed")
}

func (a *Handler) HandleAdminAPICollectionGet(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminCollection(r.Context(), r.PathValue("collection"))
	if err != nil {
		if errors.Is(err, store.ErrAdminResourceNotFound) {
			httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "collection was not found"))
		} else {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("collection_failed", err.Error()))
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
}

func (a *Handler) HandleAdminAPICollectionSave(w http.ResponseWriter, r *http.Request) {
	var input store.AdminCollectionMetadataInput
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if input.ExpectedEditVersion == nil {
		httpx.WriteJSON(w, 428, httpx.ErrorBody("edit_version_required", "expected_edit_version is required"))
		return
	}
	input.CreatedBy = a.actor(r).UserID
	revision, err := a.store.AdminUpdateCollectionMetadata(r.Context(), r.PathValue("collection"), input)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("collection_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "revision": revision})
}

var errCollectionRejectReasonRequired = errors.New("拒绝合集修订必须填写理由，创作者需要知道要改什么")
