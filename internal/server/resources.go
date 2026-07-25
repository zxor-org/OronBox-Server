package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/model"
)

type userContextKey struct{}

func (a *App) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, errorBody("unauthorized", "OronBox access token is required"))
			return
		}
		user, err := a.store.UserByAccessToken(r.Context(), authcore.HashToken(strings.TrimSpace(header[7:]), a.cfg.SessionSecret))
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errorBody("unauthorized", "OronBox access token is invalid or expired"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

func currentUser(r *http.Request) model.User {
	return r.Context().Value(userContextKey{}).(model.User)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (a *App) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if err := a.store.RevokeSession(r.Context(), authcore.HashToken(strings.TrimSpace(header[7:]), a.cfg.SessionSecret)); err != nil {
		writeJSON(w, http.StatusUnauthorized, errorBody("unauthorized", "OronBox session could not be revoked"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := a.store.Grants(r.Context(), currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("grants_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

func (a *App) handleListResources(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 100 {
		limit = value
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	devices := strings.FieldsFunc(r.URL.Query().Get("devices"), func(r rune) bool { return r == ',' })
	resources, total, err := a.creator.PublicResources(r.Context(), creator.PublicQuery{
		Limit: limit, Offset: offset, Search: strings.TrimSpace(r.URL.Query().Get("query")),
		Kind: strings.TrimSpace(r.URL.Query().Get("type")), Sort: r.URL.Query().Get("sort"), Devices: devices,
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("list_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": resources, "total": total})
}

func (a *App) handleResource(w http.ResponseWriter, r *http.Request) {
	resource, err := a.creator.PublicResource(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("resource_not_found", "resource was not found"))
		return
	}
	writeJSON(w, http.StatusOK, resource)
}

func (a *App) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := a.creator.Devices(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("devices_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (a *App) handleBlob(w http.ResponseWriter, r *http.Request) {
	digest := r.PathValue("sha256")
	if !sha256Pattern.MatchString(digest) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_blob", "invalid SHA-256"))
		return
	}
	record, err := a.store.PublicBlob(r.Context(), digest)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("blob_not_found", "blob was not found"))
		return
	}
	line := r.URL.Query().Get("line")
	r2Ready := record.R2State == "ready" && record.R2Key != "" && a.cfg.Storage.R2.PublicBaseURL != ""
	if line != "local" && r2Ready {
		http.Redirect(w, r, a.cfg.Storage.R2.PublicBaseURL+"/"+record.R2Key, http.StatusTemporaryRedirect)
		return
	}
	if line == "r2" {
		writeJSON(w, http.StatusServiceUnavailable, errorBody("r2_unavailable", "R2 replica is not available for this blob"))
		return
	}
	reader, err := a.blobs.Open(r.Context(), record.LocalKey)
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("blob_not_found", "local blob was not found"))
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", record.MediaType)
	w.Header().Set("ETag", `"`+record.SHA256+`"`)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, record.SHA256, time.Time{}, reader)
}

func decodeJSON(r *http.Request, destination any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
