package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func isAdminDraft(rev store.AdminRevision) bool {
	return rev.State == "draft" && rev.CreatedVia == "admin"
}

func pickWorkingRevision(revisions []store.AdminRevision, currentID string) store.AdminRevision {
	for _, rev := range revisions {
		if isAdminDraft(rev) {
			return rev
		}
	}
	for _, rev := range revisions {
		if isPendingReview(rev) {
			return rev
		}
	}
	if currentID != "" {
		for _, rev := range revisions {
			if rev.ID == currentID {
				return rev
			}
		}
	}
	if len(revisions) == 0 {
		return store.AdminRevision{}
	}
	return revisions[0]
}

func (a *Handler) HandleAdminAPIResource(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminResource(r.Context(), r.PathValue("resource"))
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "resource was not found"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("resource_failed", err.Error()))
		return
	}
	revision := pickWorkingRevision(detail.Revisions, detail.Resource.CurrentRevisionID)
	revDetail, err := a.store.AdminResourceRevision(r.Context(), detail.Resource.ID, revision.ID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("resource_failed", err.Error()))
		return
	}
	governance, _ := a.store.AdminRevisionGovernance(r.Context(), revision.ID)
	attributes, _ := a.creator.Attributes(r.Context(), true)
	devices, _ := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Page: 1, PerPage: 200})
	collections, _ := a.store.AdminCollections(r.Context(), store.AdminCollectionQuery{Page: 1, PerPage: 100})
	revisions := make([]map[string]any, 0, len(detail.Revisions))
	for _, item := range detail.Revisions {
		revisions = append(revisions, map[string]any{
			"id": item.ID, "number": item.Number, "name": item.Name, "state": item.State, "created_via": item.CreatedVia,
			"review_state": item.ReviewState, "created_at": item.CreatedAt,
		})
	}
	workspace, err := a.creator.Workspace(r.Context(), detail.Resource.OwnerID, detail.Resource.ID)
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("resource_failed", "resource unavailable"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"edit_version": workspace.Resource.EditVersion,
		"id":           detail.Resource.ID, "name": revision.Name, "summary": revision.Summary, "description": revision.Description, "description_format": revision.DescriptionFormat, "paid_type": revision.PaidType,
		"slug": detail.Resource.Slug, "kind": detail.Resource.Kind, "platform": detail.Resource.Platform,
		"owner": detail.Resource.Owner, "owner_id": detail.Resource.OwnerID,
		"moderation": detail.Resource.ModerationState, "moderation_reason": detail.Resource.ModerationReason,
		"revision_id": revision.ID, "base_revision_id": revision.BaseRevisionID, "revision_state": revision.State,
		"can_submit": isAdminDraft(revision), "pending": isPendingReview(revision),
		"editable": isAdminDraft(revision) || isPendingReview(revision), "attributes": revision.Attributes,
		"links": revDetail.Links, "publication_plan": json.RawMessage(revision.PublicationPlan),
		"governance": governance, "media": mediaJSON(revDetail.Media), "artifacts": artifactJSON(revDetail.Artifacts),
		"revisions": revisions, "publications": detail.Publications, "bindings": detail.Bindings, "events": detail.Events,
		"attribute_catalog": attributes, "devices": devices.Items, "collections": collections.Items,
	})
}

func (a *Handler) HandleAdminAPIResourceDraft(w http.ResponseWriter, r *http.Request) {
	expected, ok := httpx.EditVersion(w, r)
	if !ok {
		return
	}
	var body struct {
		Name              string            `json:"name"`
		Summary           string            `json:"summary"`
		Description       *string           `json:"description"`
		DescriptionFormat string            `json:"description_format"`
		PaidType          string            `json:"paid_type"`
		RevisionID        string            `json:"revision_id"`
		Attributes        []string          `json:"attributes"`
		Links             []store.AdminLink `json:"links"`
		PublicationPlan   json.RawMessage   `json:"publication_plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	resourceID := r.PathValue("resource")
	full, err := a.store.AdminResource(r.Context(), resourceID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("draft_failed", err.Error()))
		return
	}
	working := pickWorkingRevision(full.Revisions, full.Resource.CurrentRevisionID)
	sourceID := working.ID
	if sourceID == "" {
		sourceID = strings.TrimSpace(body.RevisionID)
	}
	detail, err := a.store.AdminResourceRevision(r.Context(), resourceID, sourceID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("draft_failed", err.Error()))
		return
	}
	draftID, baseID := sourceID, detail.Revision.BaseRevisionID
	editable := isAdminDraft(detail.Revision) || isPendingReview(detail.Revision)
	if !editable {
		draftID = ""
		baseID = detail.Revision.ID
	} else if baseID == "" {
		baseID = detail.Revision.ID
	}
	links := body.Links
	if body.Links == nil {
		links = detail.Links
	}
	plan := body.PublicationPlan
	if len(plan) == 0 {
		plan = append(json.RawMessage(nil), detail.Revision.PublicationPlan...)
	}
	description, format := detail.Revision.Description, detail.Revision.DescriptionFormat
	if body.Description != nil {
		description = *body.Description
		format = body.DescriptionFormat
		if format == "" {
			format = "markdown"
		}
	}
	actor := a.actor(r)
	revisionID, err := a.store.AdminSaveRevisionDraft(r.Context(), resourceID, store.AdminRevisionDraftInput{
		ExpectedEditVersion: &expected,
		DraftRevisionID:     draftID,
		BaseRevisionID:      baseID,
		Name:                body.Name,
		Summary:             body.Summary,
		Description:         description, DescriptionFormat: format,
		PaidType:        body.PaidType,
		Attributes:      body.Attributes,
		Links:           links,
		PublicationPlan: plan,
	}, actor)
	if err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("draft_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "revision_id": revisionID, "edit_version": expected + 1})
}

func (a *Handler) HandleAdminAPIResourceSubmit(w http.ResponseWriter, r *http.Request) {
	actor := a.actor(r)
	expected, ok := httpx.EditVersion(w, r)
	if !ok {
		return
	}
	if err := a.store.AdminSubmitRevisionDraftAt(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor, expected); err != nil {
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("submit_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIResourceState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	actor := a.actor(r)
	result, err := a.store.AdminManageResource(r.Context(), r.PathValue("resource"), body.Action, body.Reason, actor)
	if err != nil {
		a.audit(r, "resource."+body.Action, "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("resource_failed", err.Error()))
		return
	}
	a.audit(r, "resource."+body.Action, "success", "resource="+result.ID+" moderation="+result.ModerationState)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": result.Deleted})
}

func (a *Handler) HandleAdminAPIResourceDiscard(w http.ResponseWriter, r *http.Request) {
	actor := a.actor(r)
	if err := a.store.AdminDiscardRevisionDraft(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor); err != nil {
		a.audit(r, "resource.draft.discard", "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("draft_failed", err.Error()))
		return
	}
	a.audit(r, "resource.draft.discard", "success", "revision="+r.PathValue("revision"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIResourceRollback(w http.ResponseWriter, r *http.Request) {
	actor := a.actor(r)
	id, err := a.store.AdminCreateRollbackRevision(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor)
	if err != nil {
		a.audit(r, "resource.rollback", "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("rollback_failed", err.Error()))
		return
	}
	a.audit(r, "resource.rollback", "success", "revision="+id)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "revision_id": id})
}

func (a *Handler) HandleAdminAPIResourceGovernance(w http.ResponseWriter, r *http.Request) {
	var input store.AdminRevisionGovernance
	if err := httpx.ReadJSON(r, &input); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.store.AdminSaveRevisionGovernance(r.Context(), r.PathValue("resource"), r.PathValue("revision"), input); err != nil {
		a.audit(r, "resource.governance", "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("governance_failed", err.Error()))
		return
	}
	a.audit(r, "resource.governance", "success", "revision="+r.PathValue("revision"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIResources(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminResources(r.Context(), store.AdminResourceQuery{
		Search:            r.URL.Query().Get("q"),
		Kind:              r.URL.Query().Get("kind"),
		Owner:             r.URL.Query().Get("owner"),
		Moderation:        r.URL.Query().Get("state"),
		PublicationTarget: r.URL.Query().Get("target"),
		Sort:              r.URL.Query().Get("sort"),
		Page:              httpx.PositiveInt(r.URL.Query().Get("page"), 1),
		PerPage:           httpx.PositiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("resources_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id":                item.ID,
			"name":              item.Name,
			"slug":              item.Slug,
			"kind":              item.Kind,
			"platform":          item.Platform,
			"owner":             item.Owner,
			"moderation":        item.ModerationState,
			"moderation_reason": item.ModerationReason,
			"revision_name":     item.CurrentRevisionName,
			"revision_number":   item.CurrentRevisionNumber,
			"revision_state":    item.CurrentRevisionState,
			"review_state":      item.LatestReviewState,
			"targets":           item.Targets,
			"updated_at":        item.UpdatedAt,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": page.Total, "page": page.Page, "per_page": page.PerPage,
	})
}
