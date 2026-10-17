package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleCreatorList(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.List(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"resources": items})
}

func (a *App) handleCreatorReviewList(w http.ResponseWriter, r *http.Request) {
	limit := 30
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	page, err := a.creator.ReviewPage(r.Context(), currentUser(r).ID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func (a *App) handleCreatorReview(w http.ResponseWriter, r *http.Request) {
	workspace, err := a.creator.ReviewWorkspace(r.Context(), currentUser(r).ID, r.PathValue("review"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, workspace)
}

func (a *App) handleCreatorCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Slug string               `json:"slug"`
		Name string               `json:"name"`
		Kind creator.ResourceKind `json:"kind"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	workspace, err := a.creator.Create(r.Context(), currentUser(r).ID, request.Slug, request.Name, request.Kind)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, workspace)
}

func (a *App) handleCreatorWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace, err := a.creator.Workspace(r.Context(), currentUser(r).ID, r.PathValue("resource"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, workspace)
}

// handleCreatorPublish submits the exact saved revision with a matching edit version.
func (a *App) handleCreatorPublish(w http.ResponseWriter, r *http.Request) {
	expected, ok := httpx.EditVersion(w, r)
	if !ok {
		return
	}
	var input struct {
		RevisionID string `json:"revision_id"`
	}
	if err := httpx.DecodeJSON(r, &input); err != nil || input.RevisionID == "" {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "saved revision_id is required"))
		return
	}
	workspace, err := a.creator.SubmitDraftAt(r.Context(), currentUser(r).ID, r.PathValue("resource"), input.RevisionID, expected)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, workspace)
}

func (a *App) handleCreatorDraft(w http.ResponseWriter, r *http.Request) {
	limit := a.cfg.Limits.UploadMaxBytes + 1
	bundle, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	if int64(len(bundle)) > a.cfg.Limits.UploadMaxBytes {
		httpx.WriteJSON(w, http.StatusRequestEntityTooLarge, httpx.ErrorBody("creator_invalid", "draft bundle exceeds the upload limit"))
		return
	}
	expected, ok := httpx.EditVersion(w, r)
	if !ok {
		return
	}
	workspace, err := a.creator.SaveDraftAt(r.Context(), currentUser(r).ID, r.PathValue("resource"), bundle, expected)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, workspace)
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
	httpx.WriteJSON(w, http.StatusOK, workspace)
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
	httpx.WriteJSON(w, http.StatusOK, result)
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
	httpx.WriteJSON(w, status, httpx.ErrorBody(code, err.Error()))
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

func (a *App) handleCreatorPublicationRetry(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.RetryOwnedPublication(r.Context(), currentUser(r).ID, r.PathValue("publication"))
	if errors.Is(err, store.ErrAdminPublicationNotFound) {
		httpx.WriteJSON(w, 404, httpx.ErrorBody("not_found", "publication not found"))
		return
	}
	if errors.Is(err, store.ErrAdminPublicationConflict) {
		httpx.WriteJSON(w, 409, httpx.ErrorBody("publication_conflict", "only inactive failed tasks on the current visible approved revision may be retried"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("retry_failed", "could not requeue publication"))
		return
	}
	httpx.WriteJSON(w, 200, item)
}

func (a *App) deletionListResponse(w http.ResponseWriter, r *http.Request, owner string) {
	limit := httpx.PositiveInt(r.URL.Query().Get("per_page"), 25)
	if limit > 100 {
		limit = 100
	}
	page := httpx.PositiveInt(r.URL.Query().Get("page"), 1)
	items, err := a.creator.DeletionOperations(r.Context(), owner, limit, (page-1)*limit)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items, "page": page, "per_page": limit, "has_more": len(items) == limit})
}
