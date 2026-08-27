package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
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
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
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
		if user.BannedAt != nil {
			writeJSON(w, http.StatusForbidden, errorBody("banned", "account is banned: "+user.BanReason))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

// requireCreator rejects users whose creator capability is frozen.
func (a *App) requireCreator(next http.HandlerFunc) http.Handler {
	return a.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).CreatorFrozenAt != nil {
			writeJSON(w, http.StatusForbidden, errorBody("creator_frozen", "creator capability is frozen by an administrator"))
			return
		}
		next.ServeHTTP(w, r)
	}))
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
	attributes := strings.FieldsFunc(r.URL.Query().Get("attributes"), func(r rune) bool { return r == ',' })
	seed, _ := strconv.ParseInt(r.URL.Query().Get("seed"), 10, 64)
	resources, total, err := a.creator.PublicResources(r.Context(), creator.PublicQuery{
		Limit: limit, Offset: offset, Seed: seed, Search: strings.TrimSpace(r.URL.Query().Get("query")),
		Kind: strings.TrimSpace(r.URL.Query().Get("type")), Sort: r.URL.Query().Get("sort"), Devices: devices, Attributes: attributes,
		HidePaid: r.URL.Query().Get("hide_paid") == "1", HideForcePaid: r.URL.Query().Get("hide_force_paid") == "1",
		Featured: r.URL.Query().Get("featured") == "1",
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("list_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resources": resources, "total": total})
}

func (a *App) handleResourceAttributes(w http.ResponseWriter, r *http.Request) {
	attributes, err := a.creator.Attributes(r.Context(), false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("attributes_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attributes": attributes})
}

func (a *App) handleResource(w http.ResponseWriter, r *http.Request) {
	resource, err := a.creator.PublicResource(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("resource_not_found", "resource was not found"))
		return
	}
	writeJSON(w, http.StatusOK, resource)
}

func (a *App) handleCollections(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.PublicCollections(r.Context(), r.URL.Query().Get("kind"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("collections_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *App) handleCollection(w http.ResponseWriter, r *http.Request) {
	item, err := a.creator.PublicCollection(r.Context(), r.PathValue("id"))
	if errors.Is(err, creator.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("collection_not_found", err.Error()))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("collection_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, item)
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

// handleBlob is the single choke point for public content. Preview images,
// icons, covers and banners are served without the download budget. Published
// packages still pay the per-IP rate limit and the per-user daily quota.
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
	ip := a.clientIP(r)
	if !a.allowPublicBlobDownload(ip, record.Artifact) {
		writeJSON(w, http.StatusTooManyRequests, errorBody("rate_limited", "too many downloads, slow down"))
		return
	}
	if record.Artifact {
		var userID string
		if header := strings.TrimSpace(r.Header.Get("Authorization")); len(header) >= 8 && strings.EqualFold(header[:7], "Bearer ") {
			if user, err := a.store.UserByAccessToken(r.Context(), authcore.HashToken(strings.TrimSpace(header[7:]), a.cfg.SessionSecret)); err == nil {
				userID = user.ID
			}
		}
		ipHash := fmt.Sprintf("%x", sha256.Sum256([]byte(ip+"|"+a.cfg.SessionSecret)))
		if err := a.store.RecordDownload(r.Context(), digest, userID, ipHash, a.cfg.Limits.DownloadDailyLimit); errors.Is(err, store.ErrDownloadQuota) {
			writeJSON(w, http.StatusTooManyRequests, errorBody("quota_exceeded", err.Error()))
			return
		} else if err != nil {
			observability.From(r.Context()).With("component", "downloads").Warn("record download failed", "error", err)
		}
	}
	line := r.URL.Query().Get("line")
	r2Ready := record.R2State == "ready" && record.R2Key != "" && a.r2 != nil
	if line != "local" && r2Ready {
		url, err := a.r2.PresignGet(r.Context(), record.R2Key, a.cfg.Limits.DownloadPresignTTL)
		if err != nil {
			observability.From(r.Context()).With("component", "downloads").Error("presign R2 download failed", "key", record.R2Key, "error", err)
		} else {
			http.Redirect(w, r, url, http.StatusTemporaryRedirect)
			return
		}
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
