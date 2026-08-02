package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/creator"
)

func (a *App) handleCreatorCollectionList(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.ListCollections(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *App) handleCreatorCollectionCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Slug    string               `json:"slug"`
		Name    string               `json:"name"`
		Summary string               `json:"summary"`
		Kind    creator.ResourceKind `json:"kind"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	item, err := a.creator.CreateCollection(r.Context(), currentUser(r).ID, request.Slug, request.Name, request.Summary, request.Kind)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *App) handleCreatorCollection(w http.ResponseWriter, r *http.Request) {
	item, err := a.creator.Collection(r.Context(), currentUser(r).ID, r.PathValue("collection"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleCreatorCollectionUpdate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	item, err := a.creator.UpdateCollectionMetadata(r.Context(), currentUser(r).ID, r.PathValue("collection"), request.Name, request.Summary)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleCreatorCollectionResources(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ResourceIDs              []string `json:"resource_ids"`
		RepresentativeResourceID string   `json:"representative_resource_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if err := a.creator.SetCollectionResources(r.Context(), currentUser(r).ID, r.PathValue("collection"), request.RepresentativeResourceID, request.ResourceIDs); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorCollectionDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.DeleteCollection(r.Context(), currentUser(r).ID, r.PathValue("collection")); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorRelationships(w http.ResponseWriter, r *http.Request) {
	resourceID := r.PathValue("resource")
	if _, err := a.creator.Workspace(r.Context(), currentUser(r).ID, resourceID); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	collaborators, source, err := a.creator.ResourceRelationships(r.Context(), resourceID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collaborators": collaborators, "source": source})
}

func (a *App) handleCollaborationInvitations(w http.ResponseWriter, r *http.Request) {
	invitations, err := a.creator.CollaborationInvitations(r.Context(), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("collaborations_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invitations": invitations})
}

func (a *App) handleCreatorCollaboratorInvite(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BandBBSUserID int64 `json:"bandbbs_user_id"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if err := a.creator.InviteCollaborator(r.Context(), currentUser(r).ID, r.PathValue("resource"), request.BandBBSUserID); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCollaboratorAccept(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.AcceptCollaborator(r.Context(), currentUser(r).ID, r.PathValue("resource")); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCollaboratorDecline(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := a.creator.RemoveCollaborator(r.Context(), user.ID, r.PathValue("resource"), user.ID); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCollaboratorRemove(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.RemoveCollaborator(r.Context(), currentUser(r).ID, r.PathValue("resource"), r.PathValue("user")); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorSource(w http.ResponseWriter, r *http.Request) {
	var request creator.ResourceSource
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if err := a.creator.SetResourceSource(r.Context(), currentUser(r).ID, r.PathValue("resource"), request); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleCreatorList(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.List(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": items})
}

func (a *App) handleCreatorCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Slug string               `json:"slug"`
		Name string               `json:"name"`
		Kind creator.ResourceKind `json:"kind"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	workspace, err := a.creator.Create(r.Context(), currentUser(r).ID, request.Slug, request.Name, request.Kind)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspace)
}

func (a *App) handleCreatorWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace, err := a.creator.Workspace(r.Context(), currentUser(r).ID, r.PathValue("resource"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

// handleCreatorPublish accepts one zip bundle (manifest.json + payloads) and
// atomically creates the next revision with its review case and publications.
func (a *App) handleCreatorPublish(w http.ResponseWriter, r *http.Request) {
	limit := a.cfg.Limits.UploadMaxBytes + 1
	bundle, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if int64(len(bundle)) > a.cfg.Limits.UploadMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody("creator_invalid", "publish bundle exceeds the upload limit"))
		return
	}
	workspace, err := a.creator.Publish(r.Context(), currentUser(r).ID, r.PathValue("resource"), bundle)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspace)
}

func (a *App) handleCreatorDraft(w http.ResponseWriter, r *http.Request) {
	limit := a.cfg.Limits.UploadMaxBytes + 1
	bundle, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	if int64(len(bundle)) > a.cfg.Limits.UploadMaxBytes {
		writeJSON(w, http.StatusRequestEntityTooLarge, errorBody("creator_invalid", "draft bundle exceeds the upload limit"))
		return
	}
	workspace, err := a.creator.SaveDraft(r.Context(), currentUser(r).ID, r.PathValue("resource"), bundle)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (a *App) handleCreatorTakedown(w http.ResponseWriter, r *http.Request) {
	a.writeCreatorModeration(w, r, "takedown")
}

func (a *App) handleCreatorRestore(w http.ResponseWriter, r *http.Request) {
	a.writeCreatorModeration(w, r, "restore")
}

func (a *App) writeCreatorModeration(w http.ResponseWriter, r *http.Request, action string) {
	workspace, err := a.creator.SetModeration(r.Context(), currentUser(r).ID, r.PathValue("resource"), action)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (a *App) handleCreatorDelete(w http.ResponseWriter, r *http.Request) {
	var deleteExternal []string
	for _, provider := range strings.Split(r.URL.Query().Get("delete_external"), ",") {
		if provider = strings.TrimSpace(provider); provider != "" {
			deleteExternal = append(deleteExternal, provider)
		}
	}
	result, err := a.creator.Delete(r.Context(), currentUser(r).ID, r.PathValue("resource"), deleteExternal)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *App) writeCreatorError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "creator_failed"
	switch {
	case errors.Is(err, creator.ErrNotFound):
		status, code = http.StatusNotFound, "creator_not_found"
	case errors.Is(err, creator.ErrConflict):
		status, code = http.StatusConflict, "creator_conflict"
	case errors.Is(err, creator.ErrInvalid):
		status, code = http.StatusBadRequest, "creator_invalid"
	}
	writeJSON(w, status, errorBody(code, err.Error()))
}

func (a *App) handleCreatorBlob(w http.ResponseWriter, r *http.Request) {
	reader, mediaType, err := a.creator.OpenBlob(r.Context(), currentUser(r).ID, r.PathValue("resource"), r.PathValue("sha256"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, r.PathValue("sha256"), time.Time{}, reader)
}
