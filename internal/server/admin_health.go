package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

const cleanupPreviewTTL = 10 * time.Minute

type cleanupPreviewToken struct {
	Preview   store.AdminCleanupPreview `json:"preview"`
	ActorID   string                    `json:"actor_id"`
	ExpiresAt time.Time                 `json:"expires_at"`
}

func (a *App) cleanupTokenKey() []byte {
	return []byte("oronbox-admin-cleanup-preview\x00" + a.cfg.EncryptionKey)
}

func (a *App) signCleanupPreview(payload cleanupPreviewToken) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, a.cleanupTokenKey())
	_, _ = mac.Write(raw)
	return base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (a *App) verifyCleanupPreview(token, actorID string, now time.Time) (store.AdminCleanupPreview, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return store.AdminCleanupPreview{}, fmt.Errorf("invalid preview token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return store.AdminCleanupPreview{}, fmt.Errorf("invalid preview token")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return store.AdminCleanupPreview{}, fmt.Errorf("invalid preview token")
	}
	mac := hmac.New(sha256.New, a.cleanupTokenKey())
	_, _ = mac.Write(raw)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return store.AdminCleanupPreview{}, fmt.Errorf("invalid preview signature")
	}
	var payload cleanupPreviewToken
	if err := json.Unmarshal(raw, &payload); err != nil {
		return store.AdminCleanupPreview{}, fmt.Errorf("invalid preview payload")
	}
	if payload.ActorID == "" || payload.ActorID != actorID {
		return store.AdminCleanupPreview{}, fmt.Errorf("preview belongs to another administrator")
	}
	if payload.ExpiresAt.IsZero() || !now.UTC().Before(payload.ExpiresAt.UTC()) {
		return store.AdminCleanupPreview{}, fmt.Errorf("preview expired")
	}
	if payload.Preview.Cutoff.IsZero() || payload.Preview.Cutoff.After(now.UTC()) {
		return store.AdminCleanupPreview{}, fmt.Errorf("invalid cleanup cutoff")
	}
	return payload.Preview, nil
}

func cleanupConfirmation(preview store.AdminCleanupPreview) string {
	return fmt.Sprintf("清理 %d 条过期记录", preview.Total())
}

func cleanupSummary(stats store.CleanupStats) string {
	return fmt.Sprintf("states=%d tickets=%d admin_sessions=%d messages=%d", stats.OAuthStates, stats.LoginTickets, stats.AdminSessions, stats.UserMessages)
}
func (a *App) handleAdminCleanupPreview(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	preview, err := a.store.PreviewExpiredCleanup(r.Context(), time.Now().UTC())
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "cleanup.preview", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusInternalServerError, errorBody("cleanup_preview_failed", err.Error()))
		return
	}
	token, err := a.signCleanupPreview(cleanupPreviewToken{Preview: preview, ActorID: actor.UserID, ExpiresAt: time.Now().UTC().Add(cleanupPreviewTTL)})
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "cleanup.preview", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusInternalServerError, errorBody("cleanup_preview_failed", "无法生成清理预览"))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "cleanup.preview", "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("cutoff=%s total=%d %s", preview.Cutoff.Format(time.RFC3339Nano), preview.Total(), cleanupSummary(store.CleanupStats{OAuthStates: preview.OAuthStates, LoginTickets: preview.LoginTickets, AdminSessions: preview.AdminSessions, UserMessages: preview.UserMessages})))
	writeJSON(w, http.StatusOK, map[string]any{"preview": preview, "token": token, "confirmation": cleanupConfirmation(preview)})
}

func (a *App) handleAdminCleanupExecute(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	if err := r.ParseForm(); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "cleanup.execute", "failure", a.clientIP(r), r.UserAgent(), "invalid form")
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	preview, err := a.verifyCleanupPreview(strings.TrimSpace(r.FormValue("preview_token")), actor.UserID, time.Now().UTC())
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "cleanup.execute", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, "清理预览无效或已过期，请重新预览", http.StatusBadRequest)
		return
	}
	if r.FormValue("confirmation") != cleanupConfirmation(preview) {
		_ = a.store.RecordAudit(r.Context(), actor, "cleanup.execute", "failure", a.clientIP(r), r.UserAgent(), fmt.Sprintf("confirmation mismatch cutoff=%s total=%d", preview.Cutoff.Format(time.RFC3339Nano), preview.Total()))
		writeJSON(w, http.StatusBadRequest, errorBody("cleanup_confirmation_mismatch", "危险操作确认短语不匹配，未执行任何清理"))
		return
	}
	stats, err := a.store.ExecuteExpiredCleanup(r.Context(), preview)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "cleanup.execute", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusInternalServerError, errorBody("cleanup_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "cleanup.execute", "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("cutoff=%s preview_total=%d deleted_total=%d %s", preview.Cutoff.Format(time.RFC3339Nano), preview.Total(), stats.OAuthStates+stats.LoginTickets+stats.AdminSessions+stats.UserMessages, cleanupSummary(stats)))
	http.Redirect(w, r, "/admin/health?action=cleanup_complete", http.StatusFound)
}
