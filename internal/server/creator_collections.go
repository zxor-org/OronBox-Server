package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"io"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
)

func (a *App) handleCreatorCollectionList(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.ListCollections(r.Context(), currentUser(r).ID)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *App) handleCreatorCollectionCreate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Slug    string               `json:"slug"`
		Name    string               `json:"name"`
		Summary string               `json:"summary"`
		Kind    creator.ResourceKind `json:"kind"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	item, err := a.creator.CreateCollection(r.Context(), currentUser(r).ID, request.Slug, request.Name, request.Summary, request.Kind)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (a *App) handleCreatorCollection(w http.ResponseWriter, r *http.Request) {
	item, err := a.creator.Collection(r.Context(), currentUser(r).ID, r.PathValue("collection"))
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	if id := r.URL.Query().Get("revision"); id != "" {
		revision, err := a.creator.CollectionRevision(r.Context(), currentUser(r).ID, item.ID, id)
		if err != nil {
			a.writeCreatorError(w, err)
			return
		}
		found := false
		for _, v := range item.Revisions {
			if v.ID == id {
				found = true
				break
			}
		}
		if !found {
			item.Revisions = append(item.Revisions, revision)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (a *App) handleCreatorCollectionUpdate(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name    string `json:"name"`
		Summary string `json:"summary"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	item, err := a.creator.UpdateCollectionMetadata(r.Context(), currentUser(r).ID, r.PathValue("collection"), request.Name, request.Summary)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (a *App) handleCreatorCollectionResources(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ResourceIDs              []string `json:"resource_ids"`
		RepresentativeResourceID string   `json:"representative_resource_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
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

func (a *App) handleCollectionSubmit(w http.ResponseWriter, r *http.Request) {
	var in creator.CollectionSubmission
	if err := httpx.DecodeJSON(r, &in); err != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "invalid collection snapshot"))
		return
	}
	item, err := a.creator.SubmitCollection(r.Context(), currentUser(r).ID, r.PathValue("collection"), in)
	if err != nil {
		a.writeCreatorError(w, err)
		return
	}
	httpx.WriteJSON(w, 200, item)
}

func (a *App) handleCreatorMedia(w http.ResponseWriter, r *http.Request) {
	owner := currentUser(r).ID
	if r.Method == http.MethodGet {
		sha := r.PathValue("sha")
		ok, err := a.store.CreatorOwnsImage(r.Context(), owner, sha)
		if err != nil || !ok {
			httpx.WriteJSON(w, 404, httpx.ErrorBody("not_found", "image not found"))
			return
		}

		record, err := a.store.Blob(r.Context(), sha)
		if err != nil {
			httpx.WriteJSON(w, 404, httpx.ErrorBody("not_found", "image unavailable"))
			return
		}
		file, err := a.blobs.Open(r.Context(), record.LocalKey)
		if err != nil {
			httpx.WriteJSON(w, 404, httpx.ErrorBody("not_found", "image unavailable"))
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, 4<<20+1))
		if err != nil || len(data) > 4<<20 {
			httpx.WriteJSON(w, 500, httpx.ErrorBody("media_failed", "image unavailable"))
			return
		}
		httpx.WriteJSON(w, 200, map[string]string{"bytes": base64.StdEncoding.EncodeToString(data)})
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, (4<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 4<<20 {
		httpx.WriteJSON(w, 413, httpx.ErrorBody("invalid_image", "image exceeds size limit"))
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 1500 || cfg.Height > 1500 || (format != "png" && format != "jpeg" && format != "webp" && format != "gif") {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_image", "unsupported image or dimensions exceed 1500px"))
		return
	}
	obj, err := a.blobs.Put(r.Context(), bytes.NewReader(data))
	if err == nil {
		err = a.store.EnsureBlob(r.Context(), obj.SHA256, obj.Size, "image/"+format, obj.Key)
	}
	if err == nil {
		err = a.store.RegisterCreatorImage(r.Context(), owner, obj.SHA256)
	}
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("media_failed", "could not save image"))
		return
	}
	httpx.WriteJSON(w, 201, map[string]any{"sha256": obj.SHA256, "width": cfg.Width, "height": cfg.Height})
}
