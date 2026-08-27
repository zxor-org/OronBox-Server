package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

var errCollectionRejectReasonRequired = errors.New("拒绝合集修订必须填写理由，创作者需要知道要改什么")

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

func (a *App) handleAdminCollectionReviewQueue(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.CollectionReviewQueue(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("collection_review_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *App) applyCollectionReview(ctx context.Context, revisionID string, actor store.AdminSession, approve bool, note string) error {
	note = strings.TrimSpace(note)
	if !approve && note == "" {
		return errCollectionRejectReasonRequired
	}
	return a.creator.ReviewCollection(ctx, revisionID, actor.UserID, approve, note)
}

func (a *App) handleAdminCollectionReviewDecision(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	actor := currentAdmin(r)
	err := a.applyCollectionReview(r.Context(), r.PathValue("revision"), actor, request.Approve, request.Note)
	if errors.Is(err, errCollectionRejectReasonRequired) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusConflict, errorBody("collection_review_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "success", a.clientIP(r), r.UserAgent(), "revision="+r.PathValue("revision"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
