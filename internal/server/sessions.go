package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleTicketExchange(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	meta := a.clientMeta(r)
	var req struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Ticket) == "" {
		a.recordEvent(r, "ticket_exchange", "failure", meta, "", "", "", "", "", "invalid_request", "missing ticket", start)
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "missing ticket"))
		return
	}
	accessToken, err := authcore.RandomToken(32)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("token_create_failed", "failed to create session"))
		return
	}
	refreshToken, err := authcore.RandomToken(48)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("token_create_failed", "failed to create session"))
		return
	}
	user, tokenCipher, err := a.store.ConsumeLoginTicket(r.Context(), authcore.HashToken(strings.TrimSpace(req.Ticket), a.cfg.SessionSecret), store.SessionParams{
		AccessHash: authcore.HashToken(accessToken, a.cfg.SessionSecret), RefreshHash: authcore.HashToken(refreshToken, a.cfg.SessionSecret),
		AccessExpiresAt: time.Now().UTC().Add(a.cfg.AccessTokenTTL), RefreshExpiresAt: time.Now().UTC().Add(a.cfg.RefreshTokenTTL), Meta: meta,
	})
	if err != nil {
		a.recordEvent(r, "ticket_exchange", "failure", meta, "", "", "", "", "", "invalid_ticket", err.Error(), start)
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_ticket", "ticket is invalid or expired"))
		return
	}
	response := model.SessionTokens{AccessExpiresAt: time.Now().UTC().Add(a.cfg.AccessTokenTTL), RefreshExpiresAt: time.Now().UTC().Add(a.cfg.RefreshTokenTTL), AccessToken: accessToken, RefreshToken: refreshToken, TokenType: "Bearer", ExpiresIn: int64(a.cfg.AccessTokenTTL.Seconds()), RefreshExpiresIn: int64(a.cfg.RefreshTokenTTL.Seconds()), User: user}
	if len(tokenCipher) > 0 {
		raw, err := a.secrets.Decrypt(tokenCipher)
		if err != nil {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("token_decrypt_failed", err.Error()))
			return
		}
		var payload model.TokenPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("token_decrypt_failed", err.Error()))
			return
		}
		response.BandBBS = &payload
	}
	a.recordEvent(r, "ticket_exchange", "success", meta, "", "", fmt.Sprint(user.BandBBSUserID), "", "", "", "", start)
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, response)
}

func (a *App) handleBandBBSTokenRefresh(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RejectedAccess string `json:"rejected_access_token"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "invalid refresh request"))
			return
		}
	}
	token, _, err := a.bandbbs.GrantToken(r.Context(), a.store, a.secrets, currentUser(r).ID, ProviderBandBBS, request.RejectedAccess)
	if err != nil {
		if errors.Is(err, bandbbs.ErrReauthorization) {
			httpx.WriteJSON(w, 403, httpx.ErrorBody("bandbbs_reauthorization_required", "请重新授权米坛账号"))
		} else {
			httpx.WriteJSON(w, 503, httpx.ErrorBody("bandbbs_refresh_unavailable", "米坛授权刷新暂时失败，请重试"))
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 200, token)
}

func (a *App) handleRefresh(w http.ResponseWriter, r *http.Request) {
	meta := a.clientMeta(r)
	var req struct {
		RefreshToken string `json:"refresh_token"`
		RotationID   string `json:"rotation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "missing refresh_token"))
		return
	}
	accessToken, err := authcore.RandomToken(32)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("token_create_failed", "failed to create session"))
		return
	}
	refreshToken, err := authcore.RandomToken(48)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("token_create_failed", "failed to create session"))
		return
	}
	response := model.SessionTokens{AccessExpiresAt: time.Now().UTC().Add(a.cfg.AccessTokenTTL), RefreshExpiresAt: time.Now().UTC().Add(a.cfg.RefreshTokenTTL), AccessToken: accessToken, RefreshToken: refreshToken, TokenType: "Bearer", ExpiresIn: int64(a.cfg.AccessTokenTTL.Seconds()), RefreshExpiresIn: int64(a.cfg.RefreshTokenTTL.Seconds())}
	raw, err := json.Marshal(response)
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("refresh_unavailable", "session refresh unavailable"))
		return
	}
	cipher, err := a.secrets.Encrypt(string(raw))
	if err != nil {
		httpx.WriteJSON(w, 500, httpx.ErrorBody("refresh_unavailable", "session refresh unavailable"))
		return
	}
	var requestHash []byte
	if req.RotationID != "" {
		if len(req.RotationID) > 128 {
			httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "invalid rotation id"))
			return
		}
		requestHash = authcore.HashToken(req.RotationID, a.cfg.SessionSecret)
	}
	user, receipt, err := a.store.RotateSessionWithReceipt(r.Context(), authcore.HashToken(strings.TrimSpace(req.RefreshToken), a.cfg.SessionSecret), store.SessionParams{
		RotationRequestHash: requestHash, RotationResponseCipher: cipher, AccessHash: authcore.HashToken(accessToken, a.cfg.SessionSecret), RefreshHash: authcore.HashToken(refreshToken, a.cfg.SessionSecret),
		AccessExpiresAt: time.Now().UTC().Add(a.cfg.AccessTokenTTL), RefreshExpiresAt: time.Now().UTC().Add(a.cfg.RefreshTokenTTL), Meta: meta,
	})
	if errors.Is(err, store.ErrInvalidCredential) {
		httpx.WriteJSON(w, http.StatusUnauthorized, httpx.ErrorBody("invalid_refresh_token", "refresh token is invalid or expired"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("refresh_unavailable", "session refresh temporarily unavailable"))
		return
	}
	if len(receipt) > 0 {
		plain, err := a.secrets.Decrypt(receipt)
		if err != nil {
			httpx.WriteJSON(w, 500, httpx.ErrorBody("refresh_unavailable", "session recovery failed"))
			return
		}
		if err = json.Unmarshal([]byte(plain), &response); err != nil {
			httpx.WriteJSON(w, 500, httpx.ErrorBody("refresh_unavailable", "session recovery failed"))
			return
		}
	}
	response.User = user
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 200, response)
}

func (a *App) createTicket(r *http.Request, meta model.ClientMeta, userID, returnURI string, tokenCipher []byte) (string, string, error) {
	ticket, err := authcore.RandomToken(32)
	if err != nil {
		return "", "", err
	}
	ticketID := shortID(ticket)
	_, err = a.store.CreateLoginTicket(r.Context(), store.LoginTicketParams{
		TicketHash: authcore.HashToken(ticket, a.cfg.SessionSecret), UserID: userID,
		ExpiresAt: time.Now().UTC().Add(a.cfg.LoginTicketTTL), Meta: meta, ReturnURI: returnURI,
		TokenCipher: tokenCipher,
	})
	return ticket, ticketID, err
}

func (a *App) revokePair(r *http.Request, token model.TokenPayload) {
	if err := a.bandbbs.Revoke(r.Context(), token.AccessToken, "access_token"); err != nil {
		oauthLog(r.Context()).Warn("access token revocation failed", "provider", ProviderBandBBS, "error", err)
	}
	if token.RefreshToken != "" {
		if err := a.bandbbs.Revoke(r.Context(), token.RefreshToken, "refresh_token"); err != nil {
			oauthLog(r.Context()).Warn("refresh token revocation failed", "provider", ProviderBandBBS, "error", err)
		}
	}
}

const (
	ProviderBandBBS = "bandbbs"
	ProviderGitHub  = "github"
)

func containsInt64(values []int64, expected int64) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func preferredAvatar(values map[string]string) string {
	for _, key := range []string{"l", "m", "h", "o", "s"} {
		if value := values[key]; value != "" {
			return value
		}
	}
	return ""
}

func mergeScopes(groups ...[]string) []string {
	seen := map[string]bool{}
	var result []string
	for _, group := range groups {
		for _, scope := range group {
			if !seen[scope] {
				seen[scope] = true
				result = append(result, scope)
			}
		}
	}
	return result
}

// oauthLog keeps request-scoped fields but files the event under the oauth
// component.
func oauthLog(ctx context.Context) *slog.Logger {
	return observability.From(ctx).With("component", "oauth")
}

func (a *App) recordEvent(r *http.Request, eventType, result string, meta model.ClientMeta, stateID, ticketID, userID, expectedScopes, actualScopes, errorCode, errorMessage string, started time.Time) {
	if expectedScopes == "" {
		expectedScopes = config.ScopeString(a.cfg.BandBBS.Scopes)
	}
	err := a.store.RecordOAuthEvent(r.Context(), model.OAuthEvent{
		Provider:       ProviderBandBBS,
		EventType:      eventType,
		Result:         result,
		AppID:          meta.AppID,
		AppVersion:     meta.Version,
		AppBuild:       meta.Build,
		Platform:       meta.Platform,
		IP:             meta.IP,
		UserAgent:      meta.UA,
		StateID:        stateID,
		TicketID:       ticketID,
		ProviderUserID: userID,
		ExpectedScopes: expectedScopes,
		ActualScopes:   actualScopes,
		ErrorCode:      errorCode,
		ErrorMessage:   errorMessage,
		LatencyMS:      time.Since(started).Milliseconds(),
	})
	if err != nil {
		oauthLog(r.Context()).Error("OAuth audit event could not be recorded", "event", eventType, "result", result, "error", err)
	}
	attrs := []any{
		"provider", ProviderBandBBS,
		"event", eventType,
		"result", result,
		"app_id", meta.AppID,
		"platform", meta.Platform,
		"provider_user_id", userID,
		"latency_ms", time.Since(started).Milliseconds(),
	}
	if errorCode != "" {
		attrs = append(attrs, "error_code", errorCode, "error", errorMessage)
	}
	if result == "success" {
		oauthLog(r.Context()).Info("OAuth flow event", attrs...)
	} else {
		oauthLog(r.Context()).Warn("OAuth flow event", attrs...)
	}
}

func (a *App) clientMeta(r *http.Request) model.ClientMeta {
	return model.ClientMeta{
		AppID:    firstNonEmpty(r.Header.Get("X-OronBox-App-Id"), r.URL.Query().Get("app_id"), "unknown"),
		Version:  firstNonEmpty(r.Header.Get("X-OronBox-Version"), r.URL.Query().Get("app_version"), "unknown"),
		Platform: firstNonEmpty(r.Header.Get("X-OronBox-Platform"), r.URL.Query().Get("platform"), "unknown"),
		Build:    firstNonEmpty(r.Header.Get("X-OronBox-Build"), r.URL.Query().Get("app_build"), "unknown"),
		IP:       a.clientIP(r),
		UA:       r.UserAgent(),
	}
}

func randomState() (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	buf := make([]byte, 32)
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = alphabet[int(random[i])%len(alphabet)]
	}
	return string(buf), nil
}

func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func shortID(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func appendQuery(rawURI string, key string, value string) string {
	u, err := url.Parse(rawURI)
	if err != nil {
		return rawURI
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
