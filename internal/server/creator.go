package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/creator"
)

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
		Kind creator.ResourceKind `json:"kind"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	workspace, err := a.creator.Create(r.Context(), currentUser(r).ID, request.Slug, request.Kind)
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

func (a *App) handleCreatorTakedown(w http.ResponseWriter, r *http.Request) {
	a.writeCreatorModeration(w, r, "takedown")
}

func (a *App) handleCreatorRestore(w http.ResponseWriter, r *http.Request) {
	a.writeCreatorModeration(w, r, "restore")
}

// handleCreatorArchive keeps the legacy archive endpoint working as an alias
// for the moderation transitions.
// TODO: remove once released clients no longer call PATCH .../archive.
func (a *App) handleCreatorArchive(w http.ResponseWriter, r *http.Request) {
	archived, _ := strconv.ParseBool(r.URL.Query().Get("archived"))
	action := "restore"
	if archived {
		action = "takedown"
	}
	a.writeCreatorModeration(w, r, action)
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
	if err := a.creator.Delete(r.Context(), currentUser(r).ID, r.PathValue("resource")); err != nil {
		a.writeCreatorError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
