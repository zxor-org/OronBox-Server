package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type userContextKey struct{}

func (a *App) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(header) < 8 || !strings.EqualFold(header[:7], "Bearer ") {
			httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody("unauthorized", "OronBox access token is required"))
			return
		}
		user, err := a.store.UserByAccessToken(r.Context(), authcore.HashToken(strings.TrimSpace(header[7:]), a.cfg.SessionSecret))
		if err != nil {
			if !errors.Is(err, store.ErrInvalidCredential) {
				httpx.WriteJSON(w, 503, httpx.ErrorBody("session_unavailable", "session lookup temporarily unavailable"))
				return
			}
			httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody("unauthorized", "OronBox access token is invalid or expired"))
			return
		}
		if user.BannedAt != nil {
			httpx.WriteJSON(w, http.StatusForbidden, httpx.ErrorBody("banned", "account is banned: "+user.BanReason))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	})
}

// requireCreator rejects users whose creator capability is frozen.
func (a *App) requireCreator(next http.HandlerFunc) http.Handler {
	return a.requireUser(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r).CreatorFrozenAt != nil {
			httpx.WriteJSON(w, http.StatusForbidden, httpx.ErrorBody("creator_frozen", "creator capability is frozen by an administrator"))
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func currentUser(r *http.Request) model.User {
	return r.Context().Value(userContextKey{}).(model.User)
}

func (a *App) handleMe(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, currentUser(r))
}

func (a *App) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refresh_token"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "invalid revoke request"))
			return
		}
	}
	var accessHash, refreshHash []byte
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
		accessHash = authcore.HashToken(strings.TrimSpace(header[7:]), a.cfg.SessionSecret)
	}
	if request.RefreshToken != "" {
		refreshHash = authcore.HashToken(request.RefreshToken, a.cfg.SessionSecret)
	}
	if len(accessHash) == 0 && len(refreshHash) == 0 {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "a session credential is required"))
		return
	}
	if err := a.store.RevokeSessionCredentials(r.Context(), accessHash, refreshHash); err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("revoke_unavailable", "session revoke temporarily unavailable"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) handleGrants(w http.ResponseWriter, r *http.Request) {
	grants, err := a.store.Grants(r.Context(), currentUser(r).ID)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("grants_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, grants)
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
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("list_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"resources": resources, "total": total})
}

func (a *App) handleResourceAttributes(w http.ResponseWriter, r *http.Request) {
	scene := r.URL.Query().Get("scene")
	if scene != "" && scene != "f" && scene != "c" && scene != "filter" && scene != "creator" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_scene", "scene must be f or c"))
		return
	}
	attributes, err := a.creator.PublicTags(r.Context(), scene == "c" || scene == "creator")
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("attributes_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tags": attributes})
}

func (a *App) handleResource(w http.ResponseWriter, r *http.Request) {
	resource, err := a.creator.PublicResource(r.Context(), r.PathValue("id"))
	if err != nil {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("resource_not_found", "resource was not found"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resource)
}

func (a *App) handleCollections(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.PublicCollections(r.Context(), r.URL.Query().Get("kind"))
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("collections_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *App) handleCollection(w http.ResponseWriter, r *http.Request) {
	item, err := a.creator.PublicCollection(r.Context(), r.PathValue("id"))
	if errors.Is(err, creator.ErrNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("collection_not_found", err.Error()))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("collection_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (a *App) handleDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := a.creator.Devices(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("devices_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// handleBlob is the single choke point for public content. Preview images,
// icons, covers and banners are served without the download budget. Published
// packages still pay the per-IP rate limit and the per-user daily quota.
func (a *App) handleBlob(w http.ResponseWriter, r *http.Request) {
	digest := r.PathValue("sha256")
	if !sha256Pattern.MatchString(digest) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_blob", "invalid SHA-256"))
		return
	}
	record, err := a.store.PublicBlob(r.Context(), digest)
	if err != nil {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("blob_not_found", "blob was not found"))
		return
	}
	ip := a.clientIP(r)
	if !a.allowPublicBlobDownload(ip, record.Artifact) {
		httpx.WriteJSON(w, http.StatusTooManyRequests, httpx.ErrorBody("rate_limited", "too many downloads, slow down"))
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
			httpx.WriteJSON(w, http.StatusTooManyRequests, httpx.ErrorBody("quota_exceeded", err.Error()))
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
		httpx.WriteJSON(w, http.StatusServiceUnavailable, httpx.ErrorBody("r2_unavailable", "R2 replica is not available for this blob"))
		return
	}
	reader, err := a.blobs.Open(r.Context(), record.LocalKey)
	if err != nil {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("blob_not_found", "local blob was not found"))
		return
	}
	defer reader.Close()
	w.Header().Set("Content-Type", record.MediaType)
	w.Header().Set("ETag", `"`+record.SHA256+`"`)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, record.SHA256, time.Time{}, reader)
}

const legacyNoticeID = "migration-maintenance-notice"

// Only the old catalog notice is public. It does not expose actual resources
// or turn any legacy authenticated endpoint back on.
func serveLegacyNotice(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	list := r.URL.Path == "/api/resources" || r.URL.Path == "/api/resources/"
	if !list && r.URL.Path != "/api/resources/"+legacyNoticeID {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	notice := map[string]any{
		"id":         legacyNoticeID,
		"name":       "服务器正在更新迁移 / Server maintenance",
		"kind":       "quick_app",
		"owner":      "OrPudding",
		"summary":    "服务器正在更新迁移，相关功能暂时不可用，请耐心等待维护完成。\n\nThe server is undergoing maintenance and migration. Please wait until maintenance is complete.",
		"version":    "1.0.0",
		"devices":    []string{},
		"attributes": []string{"公告", "维护"},
		"updated_at": "2026-10-16T00:00:00Z",
		"media":      []any{},
		"artifacts":  []any{},
	}
	if list {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"total": 1, "resources": []any{notice}})
	} else {
		httpx.WriteJSON(w, http.StatusOK, notice)
	}
	return true
}
