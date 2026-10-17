package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleBandBBSStart(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ReturnURI string `json:"return_uri"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid login request"))
		return
	}
	authorizationURL, err := a.startBandBBSAuthorization(r, "login", "", request.ReturnURI)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_start_failed", "could not start authorization"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"authorization_url": authorizationURL})
}

func (a *App) handleBandBBSPublishStart(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ReturnURI string `json:"return_uri"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	authorizationURL, err := a.startBandBBSAuthorization(r, "publish", currentUser(r).ID, request.ReturnURI)
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("oauth_start_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"authorization_url": authorizationURL})
}

func (a *App) startBandBBSAuthorization(r *http.Request, purpose, userID, requestedReturnURI string) (string, error) {
	start := time.Now()
	meta := a.clientMeta(r)
	state, err := randomState()
	if err != nil {
		return "", err
	}
	returnURI := a.cfg.ClientRedirectURI
	scopes := a.cfg.BandBBS.Scopes
	if purpose == "publish" {
		if len(a.cfg.BandBBS.PublishScopes) == 0 {
			return "", fmt.Errorf("BandBBS publishing scopes are not configured")
		}
		scopes = mergeScopes(a.cfg.BandBBS.Scopes, a.cfg.BandBBS.PublishScopes)
	}
	if candidate := strings.TrimSpace(requestedReturnURI); candidate != "" && a.allowedReturnURI(candidate) {
		returnURI = candidate
	}
	err = a.store.CreateState(r.Context(), store.CreateStateParams{
		ID:        state,
		Provider:  ProviderBandBBS,
		ExpiresAt: time.Now().UTC().Add(a.cfg.StateTTL),
		Meta:      meta,
		ReturnURI: returnURI,
		Purpose:   purpose,
		UserID:    userID,
	})
	if err != nil {
		a.recordEvent(r, "start", "failure", meta, state, "", "", "", "", "db_error", err.Error(), start)
		return "", err
	}
	a.recordEvent(r, "start", "success", meta, state, "", "", config.ScopeString(scopes), "", "", "", start)
	return a.bandbbs.AuthorizeURL(state, scopes), nil
}

func (a *App) allowedReturnURI(candidate string) bool {
	if candidate == a.cfg.ClientRedirectURI {
		return true
	}
	if strings.TrimRight(candidate, "/") == strings.TrimRight(a.cfg.PublicURL, "/")+"/admin" {
		return true
	}
	parsed, err := url.Parse(candidate)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return false
	}
	origin := parsed.Scheme + "://" + parsed.Host
	return slices.Contains(a.cfg.WebClientOrigins, origin)
}

func (a *App) handleBandBBSCallback(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	stateID := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || stateID == "" {
		a.recordEvent(r, "callback", "failure", a.clientMeta(r), stateID, "", "", "", "", "invalid_callback", "missing code or state", start)
		http.Redirect(w, r, "/auth/failed?error=invalid_callback", http.StatusFound)
		return
	}
	state, err := a.store.ConsumeState(r.Context(), ProviderBandBBS, stateID)
	if err != nil {
		a.recordEvent(r, "callback", "failure", a.clientMeta(r), stateID, "", "", "", "", "invalid_state", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=invalid_state", http.StatusFound)
		return
	}
	meta := model.ClientMeta{AppID: state.AppID, Version: state.AppVersion, Build: state.AppBuild, Platform: state.Platform, IP: state.IP, UA: state.UserAgent}
	token, err := a.bandbbs.ExchangeCode(r.Context(), code)
	if err != nil {
		a.recordEvent(r, "token_exchange", "failure", meta, stateID, "", "", "", "", "token_exchange_failed", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=token_exchange_failed", http.StatusFound)
		return
	}
	requiredScopes := a.cfg.BandBBS.Scopes
	grantProvider := ProviderBandBBS
	if state.Purpose == "publish" {
		requiredScopes = mergeScopes(a.cfg.BandBBS.Scopes, a.cfg.BandBBS.PublishScopes)
		grantProvider = "bandbbs_publish"
	}
	providerUserID, actualScopes, err := a.bandbbs.ValidateScopes(r.Context(), token, requiredScopes)
	if err != nil {
		a.revokePair(r, token)
		a.recordEvent(r, "scope_check", "failure", meta, stateID, "", providerUserID, "", actualScopes, "scope_mismatch", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=scope_mismatch", http.StatusFound)
		return
	}
	if state.Purpose != "publish" && bandbbs.HasWriteScopes(actualScopes) {
		a.revokePair(r, token)
		http.Redirect(w, r, "/auth/failed?error=scope_mismatch", http.StatusFound)
		return
	}
	token.Scope = actualScopes
	profile, err := a.bandbbs.Me(r.Context(), token.AccessToken)
	if err != nil {
		a.revokePair(r, token)
		a.recordEvent(r, "identity", "failure", meta, stateID, "", providerUserID, "", actualScopes, "identity_failed", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=identity_failed", http.StatusFound)
		return
	}
	if state.UserID != "" {
		expected, expectedErr := a.store.UserByID(r.Context(), state.UserID)
		if expectedErr != nil || expected.BandBBSUserID != profile.UserID {
			a.revokePair(r, token)
			a.recordEvent(r, "identity", "failure", meta, stateID, "", providerUserID, "", actualScopes, "account_mismatch", "publication authorization account does not match the signed-in user", start)
			http.Redirect(w, r, appendQuery(state.ReturnURI, "error", "account_mismatch"), http.StatusFound)
			return
		}
	}
	user, err := a.store.UpsertUser(r.Context(), store.UpsertUserParams{
		BandBBSUserID: profile.UserID,
		Username:      profile.Username,
		AvatarURL:     preferredAvatar(profile.AvatarURLs),
	})
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=identity_save_failed", http.StatusFound)
		return
	}
	// The env whitelist keeps working as the super-admin bootstrap: members
	// are promoted to the admin role on every login.
	if containsInt64(a.cfg.Admin.BandBBSUserIDs, user.BandBBSUserID) && user.Role != "admin" {
		if user, err = a.store.SetUserRole(r.Context(), user.ID, "admin"); err != nil {
			http.Redirect(w, r, "/auth/failed?error=identity_save_failed", http.StatusFound)
			return
		}
	}
	if user.BannedAt != nil {
		a.recordEvent(r, "identity", "failure", meta, stateID, "", providerUserID, "", actualScopes, "account_banned", "account is banned", start)
		http.Redirect(w, r, "/auth/failed?error=account_banned", http.StatusFound)
		return
	}
	accessCipher, err := a.secrets.Encrypt(token.AccessToken)
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
		return
	}
	refreshCipher, err := a.secrets.Encrypt(token.RefreshToken)
	if err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
		return
	}
	var grantExpiry *time.Time
	if token.ExpiresIn > 0 {
		expires := time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)
		grantExpiry = &expires
	}
	// Refresh tokens always stay server-side. The publish grant additionally
	// carries write scopes and is only ever used by the publish worker.
	if err := a.store.WithOAuthGrantLock(r.Context(), user.ID, grantProvider, func(locked *store.Store) error {
		return locked.UpsertOAuthGrant(r.Context(), store.GrantParams{
			UserID: user.ID, Provider: grantProvider, Subject: providerUserID,
			Scopes: config.ParseScopes(actualScopes), AccessTokenCipher: accessCipher,
			RefreshTokenCipher: refreshCipher, TokenType: token.TokenType, ExpiresAt: grantExpiry,
		})
	}); err != nil {
		http.Redirect(w, r, "/auth/failed?error=grant_save_failed", http.StatusFound)
		return
	}
	var tokenCipher []byte
	if state.Purpose != "publish" {
		// The login grant is read-only: hand the access token (never the
		// refresh token) to the client through the login ticket so the app
		// can query BandBBS directly.
		payload, err := json.Marshal(model.TokenPayload{
			AccessToken: token.AccessToken,
			TokenType:   token.TokenType,
			ExpiresIn:   token.ExpiresIn,
			Scope:       token.Scope,
		})
		if err != nil {
			http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
			return
		}
		tokenCipher, err = a.secrets.Encrypt(string(payload))
		if err != nil {
			http.Redirect(w, r, "/auth/failed?error=grant_encrypt_failed", http.StatusFound)
			return
		}
	}
	ticket, ticketID, err := a.createTicket(r, meta, user.ID, state.ReturnURI, tokenCipher)
	if err != nil {
		a.recordEvent(r, "ticket_create", "failure", meta, stateID, "", providerUserID, "", actualScopes, "ticket_create_failed", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=ticket_create_failed", http.StatusFound)
		return
	}
	a.recordEvent(r, "callback", "success", meta, stateID, ticketID, providerUserID, "", actualScopes, "", "", start)
	target := appendQuery(state.ReturnURI, "ticket", ticket)
	if strings.TrimRight(state.ReturnURI, "/") == strings.TrimRight(a.cfg.PublicURL, "/")+"/admin" {
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	transitionTarget, err := a.loginTransitionTarget(state.ReturnURI, ticket)
	if err != nil {
		a.recordEvent(r, "callback", "failure", meta, stateID, ticketID, providerUserID, "", actualScopes, "invalid_return_uri", err.Error(), start)
		http.Redirect(w, r, "/auth/failed?error=invalid_state", http.StatusFound)
		return
	}
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "授权完成",
		Heading:     "授权完成",
		Description: "可以返回 OronBox 继续使用",
		Target:      transitionTarget,
		Auto:        true,
		Tone:        "success",
	})
}
