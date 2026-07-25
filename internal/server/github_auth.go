package server

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleGitHubWebStart(w http.ResponseWriter, r *http.Request) {
	if a.cfg.GitHub.ClientID == "" || a.cfg.GitHub.ClientSecret == "" || a.cfg.GitHub.RedirectURI == "" {
		writeJSON(w, http.StatusServiceUnavailable, errorBody("github_not_configured", "GitHub web OAuth is not configured"))
		return
	}
	state, err := authcore.RandomToken(32)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	verifier, err := authcore.RandomToken(32)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	verifierCipher, err := a.secrets.Encrypt(verifier)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	meta := a.clientMeta(r)
	if err := a.store.CreateState(r.Context(), store.CreateStateParams{
		ID: state, Provider: ProviderGitHub, Purpose: "publish", ExpiresAt: time.Now().UTC().Add(a.cfg.StateTTL),
		Meta: meta, ReturnURI: a.cfg.PublicURL + "/auth/success", UserID: currentUser(r).ID, SecretCipher: verifierCipher,
	}); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	challenge := sha256.Sum256([]byte(verifier))
	writeJSON(w, http.StatusOK, map[string]string{
		"flow_id":           state,
		"authorization_url": a.github.AuthorizeURL(state, base64.RawURLEncoding.EncodeToString(challenge[:])),
	})
}

func (a *App) handleGitHubWebStatus(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FlowID string `json:"flow_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || strings.TrimSpace(request.FlowID) == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "flow_id is required"))
		return
	}
	login, completed, err := a.store.GitHubWebFlowStatus(r.Context(), request.FlowID, currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	if !completed {
		writeJSON(w, http.StatusAccepted, map[string]string{"state": "pending"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"state": "connected", "login": login})
}

func (a *App) handleGitHubWebCallback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	stateID := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || stateID == "" {
		http.Redirect(w, r, "/auth/failed?error=invalid_callback", http.StatusFound)
		return
	}
	state, err := a.store.ConsumeState(r.Context(), ProviderGitHub, stateID)
	if err != nil || state.UserID == "" || len(state.SecretCipher) == 0 {
		http.Redirect(w, r, "/auth/failed?error=invalid_state", http.StatusFound)
		return
	}
	verifier, err := a.secrets.Decrypt(state.SecretCipher)
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=invalid_state", http.StatusFound)
		return
	}
	token, err := a.github.ExchangeCode(r.Context(), code, verifier)
	if err != nil || token.AccessToken == "" {
		oauthLog(r.Context()).Warn("GitHub login failed", "user_id", state.UserID, "flow", "web", "stage", "token_exchange", "error", err)
		http.Redirect(w, r, "/auth/failed?error=token_exchange_failed", http.StatusFound)
		return
	}
	profile, err := a.github.User(r.Context(), token.AccessToken)
	if err != nil {
		oauthLog(r.Context()).Warn("GitHub login failed", "user_id", state.UserID, "flow", "web", "stage", "identity", "error", err)
		http.Redirect(w, r, "/auth/failed?error=identity_failed", http.StatusFound)
		return
	}
	tokenCipher, err := a.secrets.Encrypt(token.AccessToken)
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
		return
	}
	if err := a.store.CompleteGitHubWebFlow(r.Context(), stateID, state.UserID, profile.ID, profile.Login, tokenCipher, parseGitHubScopes(token.Scope)); err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_save_failed", http.StatusFound)
		return
	}
	meta := model.ClientMeta{AppID: state.AppID, Version: state.AppVersion, Build: state.AppBuild, Platform: state.Platform, IP: state.IP, UA: state.UserAgent}
	_ = a.store.RecordOAuthEvent(r.Context(), model.OAuthEvent{Provider: ProviderGitHub, EventType: "callback", Result: "success", AppID: meta.AppID, AppVersion: meta.Version, AppBuild: meta.Build, Platform: meta.Platform, IP: meta.IP, UserAgent: meta.UA, StateID: stateID, ProviderUserID: profile.Login, ActualScopes: token.Scope})
	oauthLog(r.Context()).Info("GitHub login completed", "user_id", state.UserID, "github_login", profile.Login, "flow", "web")
	http.Redirect(w, r, state.ReturnURI, http.StatusFound)
}

func parseGitHubScopes(value string) []string {
	return config.ParseScopes(strings.ReplaceAll(value, ",", " "))
}

func (a *App) handleGitHubDeviceStart(w http.ResponseWriter, r *http.Request) {
	if a.cfg.GitHub.ClientID == "" {
		writeJSON(w, http.StatusServiceUnavailable, errorBody("github_not_configured", "GitHub OAuth is not configured"))
		return
	}
	device, err := a.github.Start(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errorBody("github_oauth_failed", err.Error()))
		return
	}
	cipher, err := a.secrets.Encrypt(device.DeviceCode)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	id, err := a.store.CreateGitHubDeviceFlow(r.Context(), currentUser(r).ID, cipher, device.UserCode, device.VerificationURI, device.Interval, time.Now().UTC().Add(time.Duration(device.ExpiresIn)*time.Second))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"flow_id": id, "user_code": device.UserCode, "verification_uri": device.VerificationURI, "expires_in": device.ExpiresIn, "interval": device.Interval})
}

func (a *App) handleGitHubDevicePoll(w http.ResponseWriter, r *http.Request) {
	var request struct {
		FlowID string `json:"flow_id"`
	}
	if err := decodeJSON(r, &request); err != nil || strings.TrimSpace(request.FlowID) == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "flow_id is required"))
		return
	}
	flow, err := a.store.GitHubDeviceFlow(r.Context(), request.FlowID, currentUser(r).ID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_flow", "GitHub authorization flow is invalid or expired"))
		return
	}
	deviceCode, err := a.secrets.Decrypt(flow.DeviceCodeCipher)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	token, pollErr := a.github.Poll(r.Context(), deviceCode)
	if token.Error == "authorization_pending" || token.Error == "slow_down" {
		writeJSON(w, http.StatusAccepted, map[string]any{"state": "pending", "retry_after": flow.Interval})
		return
	}
	if pollErr != nil {
		oauthLog(r.Context()).Warn("GitHub login failed", "user_id", currentUser(r).ID, "flow", "device", "stage", "token_poll", "error", pollErr)
		writeJSON(w, http.StatusBadGateway, errorBody("github_oauth_failed", pollErr.Error()))
		return
	}
	profile, err := a.github.User(r.Context(), token.AccessToken)
	if err != nil {
		oauthLog(r.Context()).Warn("GitHub login failed", "user_id", currentUser(r).ID, "flow", "device", "stage", "identity", "error", err)
		writeJSON(w, http.StatusBadGateway, errorBody("github_oauth_failed", err.Error()))
		return
	}
	cipher, err := a.secrets.Encrypt(token.AccessToken)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	if err := a.store.CompleteGitHubDeviceFlow(r.Context(), flow.ID, currentUser(r).ID, profile.ID, profile.Login, cipher, parseGitHubScopes(token.Scope)); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("github_oauth_failed", err.Error()))
		return
	}
	oauthLog(r.Context()).Info("GitHub login completed", "user_id", currentUser(r).ID, "github_login", profile.Login, "flow", "device")
	writeJSON(w, http.StatusOK, map[string]any{"state": "connected", "login": profile.Login, "avatar_url": profile.AvatarURL})
}

func (a *App) handleGitHubGrantDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteGitHubGrant(r.Context(), currentUser(r).ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("github_oauth_failed", err.Error()))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
