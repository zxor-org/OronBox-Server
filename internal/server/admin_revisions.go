package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminRevision(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminResourceRevision(r.Context(), r.PathValue("resource"), r.PathValue("revision"))
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_revision_detail", map[string]any{"Title": detail.Revision.Name, "Detail": detail})
}

func (a *App) handleAdminResourceDraft(w http.ResponseWriter, r *http.Request) {
	resource, err := a.store.AdminResource(r.Context(), r.PathValue("resource"))
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	requestedRevisionID := strings.TrimSpace(r.URL.Query().Get("base"))
	adminDraftID := ""
	for _, revision := range resource.Revisions {
		if revision.State == "draft" && revision.CreatedVia == "admin" {
			adminDraftID = revision.ID
			break
		}
	}
	revisionID := requestedRevisionID
	if revisionID == "" {
		revisionID = adminDraftID
	}
	if revisionID == "" && len(resource.Revisions) > 0 {
		revisionID = resource.Revisions[0].ID
	}
	if revisionID == "" {
		http.Error(w, "resource has no revision to edit", http.StatusConflict)
		return
	}
	detail, err := a.store.AdminResourceRevision(r.Context(), resource.Resource.ID, revisionID)
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	isAdminDraft := detail.Revision.State == "draft" && detail.Revision.CreatedVia == "admin"
	isPendingReview := detail.Revision.State == "submitted" && detail.Revision.ReviewState == "pending"
	if !isAdminDraft && !isPendingReview {
		if adminDraftID != "" {
			detail, err = a.store.AdminResourceRevision(r.Context(), resource.Resource.ID, adminDraftID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			isAdminDraft = true
		} else {
			actor := currentAdmin(r)
			draftID, draftErr := a.store.AdminSaveRevisionDraft(r.Context(), resource.Resource.ID, store.AdminRevisionDraftInput{
				BaseRevisionID:  detail.Revision.ID,
				Name:            detail.Revision.Name,
				Summary:         detail.Revision.Summary,
				PaidType:        detail.Revision.PaidType,
				Attributes:      detail.Revision.Attributes,
				Links:           detail.Links,
				PublicationPlan: append(json.RawMessage(nil), detail.Revision.PublicationPlan...),
			}, actor)
			if draftErr != nil {
				_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.create", "failure", a.clientIP(r), r.UserAgent(), draftErr.Error())
				http.Error(w, draftErr.Error(), http.StatusConflict)
				return
			}
			_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.create", "success", a.clientIP(r), r.UserAgent(), "revision="+draftID+" base="+detail.Revision.ID)
			http.Redirect(w, r, "/admin/resources/"+resource.Resource.ID+"/draft?action=draft_created", http.StatusSeeOther)
			return
		}
	}
	attributes, err := a.creator.Attributes(r.Context(), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	devices, err := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Platform: "vela_os", State: "enabled", Page: 1, PerPage: 100})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	governance, err := a.store.AdminRevisionGovernance(r.Context(), detail.Revision.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	collections, err := a.store.AdminCollections(r.Context(), store.AdminCollectionQuery{Owner: detail.Resource.OwnerID, Kind: detail.Resource.Kind, Page: 1, PerPage: 100})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_revision_editor", map[string]any{
		"Title": "编辑 " + detail.Revision.Name, "Detail": detail, "Attributes": attributes,
		"Devices": devices.Items, "Governance": governance, "Collections": collections.Items,
		"IsDraft": isAdminDraft, "CanEditAssets": isAdminDraft || isPendingReview,
		"IsPendingReview": isPendingReview, "Action": r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminResourceGovernanceSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	position := 0
	if raw := strings.TrimSpace(r.FormValue("collection_position")); raw != "" {
		var parseErr error
		position, parseErr = strconv.Atoi(raw)
		if parseErr != nil || position < 0 {
			http.Error(w, "invalid collection position", http.StatusBadRequest)
			return
		}
	}
	input := store.AdminRevisionGovernance{AuthorName: r.FormValue("author_name"), SourceURL: r.FormValue("source_url"), LicenseName: r.FormValue("license_name"), AuthorizationNote: r.FormValue("authorization_note"), CollectionID: r.FormValue("collection_id"), CollectionPosition: position}
	for _, id := range strings.FieldsFunc(r.FormValue("collaborator_ids"), func(value rune) bool { return value == ',' || value == '\n' || value == ' ' || value == '\t' }) {
		input.CollaboratorIDs = append(input.CollaboratorIDs, id)
	}
	actor := currentAdmin(r)
	err := a.store.AdminSaveRevisionGovernance(r.Context(), r.PathValue("resource"), r.PathValue("revision"), input)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.governance.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.governance.save", "success", a.clientIP(r), r.UserAgent(), "revision="+r.PathValue("revision"))
	if reviewID := strings.TrimSpace(r.FormValue("return_review")); reviewID != "" {
		http.Redirect(w, r, "/admin/review/"+reviewID+"?action=governance_saved", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/resources/"+r.PathValue("resource")+"/draft?action=governance_saved", http.StatusFound)
}

func (a *App) handleAdminResourceDraftSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	titles, urls := r.Form["link_title"], r.Form["link_url"]
	links := make([]store.AdminLink, 0, len(titles))
	for index, title := range titles {
		linkURL := ""
		if index < len(urls) {
			linkURL = urls[index]
		}
		if strings.TrimSpace(title) == "" && strings.TrimSpace(linkURL) == "" {
			continue
		}
		links = append(links, store.AdminLink{Title: title, URL: linkURL})
	}
	input := store.AdminRevisionDraftInput{
		DraftRevisionID: strings.TrimSpace(r.FormValue("draft_revision_id")),
		BaseRevisionID:  strings.TrimSpace(r.FormValue("base_revision_id")),
		Name:            r.FormValue("name"), Summary: r.FormValue("summary"), PaidType: r.FormValue("paid_type"),
		Attributes: r.Form["attributes"], Links: links,
		PublicationPlan: json.RawMessage(strings.TrimSpace(r.FormValue("publication_plan"))),
	}
	actor := currentAdmin(r)
	user, userErr := a.store.UserByID(r.Context(), actor.UserID)
	if userErr != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if user.Role != "admin" {
		comparisonID := input.DraftRevisionID
		if comparisonID == "" {
			comparisonID = input.BaseRevisionID
		}
		detail, detailErr := a.store.AdminResourceRevision(r.Context(), r.PathValue("resource"), comparisonID)
		if detailErr != nil || !equalJSON(detail.Revision.PublicationPlan, input.PublicationPlan) {
			http.Error(w, "reviewers cannot modify publication configuration", http.StatusForbidden)
			return
		}
	}
	revisionID, err := a.store.AdminSaveRevisionDraft(r.Context(), r.PathValue("resource"), input, actor)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.save", "success", a.clientIP(r), r.UserAgent(), "revision="+revisionID)
	if reviewID := strings.TrimSpace(r.FormValue("return_review")); reviewID != "" {
		http.Redirect(w, r, "/admin/review/"+reviewID+"?action=revision_saved", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/resources/"+r.PathValue("resource")+"/draft?action=saved", http.StatusFound)
}

func equalJSON(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftNormalized, _ := json.Marshal(leftValue)
	rightNormalized, _ := json.Marshal(rightValue)
	return string(leftNormalized) == string(rightNormalized)
}

func (a *App) handleAdminResourceDraftSubmit(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	err := a.store.AdminSubmitRevisionDraft(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.submit", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.submit", "success", a.clientIP(r), r.UserAgent(), "revision="+r.PathValue("revision"))
	http.Redirect(w, r, "/admin/resources/"+r.PathValue("resource")+"?action=submitted", http.StatusFound)
}

func (a *App) handleAdminResourceDraftDiscard(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	err := a.store.AdminDiscardRevisionDraft(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.discard", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.draft.discard", "success", a.clientIP(r), r.UserAgent(), "revision="+r.PathValue("revision"))
	http.Redirect(w, r, "/admin/resources/"+r.PathValue("resource")+"?action=draft_discarded", http.StatusFound)
}

func (a *App) handleAdminResourceRollback(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	revisionID, err := a.store.AdminCreateRollbackRevision(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.rollback.create", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.rollback.create", "success", a.clientIP(r), r.UserAgent(), "revision="+revisionID+" base="+r.PathValue("revision"))
	http.Redirect(w, r, "/admin/resources/"+r.PathValue("resource")+"/draft?action=rollback_created", http.StatusFound)
}
