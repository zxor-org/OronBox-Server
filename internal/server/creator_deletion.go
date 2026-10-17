package server

import (
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
)

func (a *App) handleCreatorDeletionOperation(w http.ResponseWriter, r *http.Request) {
	a.deletionOperationResponse(w, r, currentUser(r).ID)
}

func (a *App) handleAdminDeletionOperation(w http.ResponseWriter, r *http.Request) {
	a.deletionOperationResponse(w, r, "")
}

func (a *App) deletionOperationResponse(w http.ResponseWriter, r *http.Request, owner string) {
	var op creator.DeletionOperation
	var err error
	if r.Method == http.MethodPost {
		op, err = a.creator.RetryDeletion(r.Context(), owner, r.PathValue("operation"))
	} else {
		op, err = a.creator.DeletionOperation(r.Context(), owner, r.PathValue("operation"))
	}
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, op)
}

func (a *App) handleCreatorDeletionList(w http.ResponseWriter, r *http.Request) {
	a.deletionListResponse(w, r, currentUser(r).ID)
}

func (a *App) handleAdminDeletionList(w http.ResponseWriter, r *http.Request) {
	a.deletionListResponse(w, r, "")
}
