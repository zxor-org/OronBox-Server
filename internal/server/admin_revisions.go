package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

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
