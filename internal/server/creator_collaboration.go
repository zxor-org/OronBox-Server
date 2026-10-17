package server

import (
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
)

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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"collaborators": collaborators, "source": source})
}

func (a *App) handleCollaborationInvitations(w http.ResponseWriter, r *http.Request) {
	invitations, err := a.creator.CollaborationInvitations(r.Context(), currentUser(r).ID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("collaborations_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"invitations": invitations})
}

func (a *App) handleCreatorCollaboratorInvite(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BandBBSUserID int64 `json:"bandbbs_user_id"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
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
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	if err := a.creator.SetResourceSource(r.Context(), currentUser(r).ID, r.PathValue("resource"), request); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
