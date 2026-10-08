package server

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/web"
)

type ghWebFlow struct {
	userID   string
	verifier string
	expires  time.Time
}
type ghWebDone struct {
	userID  string
	login   string
	expires time.Time
}
type ghDeviceFlow struct {
	userID     string
	deviceCode string
	interval   int
	expires    time.Time
}

// --- GitHub web (PKCE) binding: POST /oauth2/github/web/start|status, GET /oauth2/github/callback ---

func (a *application) githubWebStart(w http.ResponseWriter, r *http.Request) {
	if a.cfg.GitHub.ClientID == "" || a.cfg.GitHub.ClientSecret == "" || a.cfg.GitHub.RedirectURI == "" {
		jsonError(w, http.StatusServiceUnavailable, "github_not_configured", "github web oauth is not configured", nil)
		return
	}
	u, _ := userOf(r)
	flowID := token(24)
	verifier := token(32)
	a.states.Store("ghweb:"+flowID, ghWebFlow{userID: u.ID, verifier: verifier, expires: time.Now().Add(a.cfg.StateTTL)})
	challenge := sha256.Sum256([]byte(verifier))
	authorize := a.cfg.GitHub.AuthorizeURL +
		"?response_type=code&client_id=" + escape(a.cfg.GitHub.ClientID) +
		"&state=" + escape(flowID) +
		"&redirect_uri=" + escape(a.cfg.GitHub.RedirectURI) +
		"&code_challenge=" + base64.RawURLEncoding.EncodeToString(challenge[:]) +
		"&code_challenge_method=S256&scope=" + escape(a.cfg.GitHub.Scopes)
	jsonResponse(w, http.StatusOK, map[string]string{"flow_id": flowID, "authorization_url": authorize})
}

func (a *application) githubWebStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FlowID string `json:"flow_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	flowID := strings.TrimSpace(in.FlowID)
	if flowID == "" {
		jsonError(w, http.StatusBadRequest, "invalid_request", "flow_id is required", nil)
		return
	}
	u, _ := userOf(r)
	if v, ok := a.states.Load("ghwebdone:" + flowID); ok {
		if done, ok := v.(ghWebDone); ok && done.userID == u.ID {
			jsonResponse(w, http.StatusOK, map[string]string{"state": "connected", "login": done.login})
			return
		}
	}
	if v, ok := a.states.Load("ghweb:" + flowID); ok {
		if flow, ok := v.(ghWebFlow); ok && flow.userID == u.ID && time.Now().Before(flow.expires) {
			jsonResponse(w, http.StatusAccepted, map[string]string{"state": "pending"})
			return
		}
	}
	jsonError(w, http.StatusBadRequest, "invalid_flow", "github authorization flow is invalid or expired", nil)
}

func (a *application) githubWebCallback(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	flowID := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || flowID == "" {
		http.Redirect(w, r, "/auth/failed?error=invalid_callback", http.StatusFound)
		return
	}
	v, ok := a.states.LoadAndDelete("ghweb:" + flowID)
	flow, ok2 := v.(ghWebFlow)
	if !ok || !ok2 || time.Now().After(flow.expires) {
		http.Redirect(w, r, "/auth/failed?error=invalid_state", http.StatusFound)
		return
	}
	accessToken, e := a.githubExchangeCode(r.Context(), code, flow.verifier)
	if e != nil || accessToken == "" {
		a.recordOAuthEvent(r, "github", "callback", "failure", "token_exchange_failed")
		http.Redirect(w, r, "/auth/failed?error=token_exchange_failed", http.StatusFound)
		return
	}
	login, id, e := a.githubProfile(r.Context(), accessToken)
	if e != nil {
		a.recordOAuthEvent(r, "github", "callback", "failure", "identity_failed")
		http.Redirect(w, r, "/auth/failed?error=identity_failed", http.StatusFound)
		return
	}
	if e := a.saveGitHubGrant(r.Context(), flow.userID, id, login, accessToken); e != nil {
		a.recordOAuthEvent(r, "github", "callback", "failure", "grant_save_failed")
		http.Redirect(w, r, "/auth/failed?error=grant_save_failed", http.StatusFound)
		return
	}
	a.states.Store("ghwebdone:"+flowID, ghWebDone{userID: flow.userID, login: login, expires: time.Now().Add(a.cfg.StateTTL)})
	a.recordOAuthEvent(r, "github", "callback", "success", "")
	http.Redirect(w, r, "/auth/success", http.StatusFound)
}

// --- GitHub device flow: POST /oauth2/github/device/start|poll ---

func (a *application) githubStart(w http.ResponseWriter, r *http.Request) {
	if a.cfg.GitHub.ClientID == "" {
		jsonError(w, http.StatusServiceUnavailable, "github_not_configured", "github oauth is not configured", nil)
		return
	}
	u, _ := userOf(r)
	form := url.Values{"client_id": {a.cfg.GitHub.ClientID}, "scope": {a.cfg.GitHub.Scopes}}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, a.cfg.GitHub.DeviceCodeURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		jsonError(w, http.StatusBadGateway, "github_oauth_failed", e.Error(), nil)
		return
	}
	defer resp.Body.Close()
	var device struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if json.NewDecoder(resp.Body).Decode(&device) != nil || device.DeviceCode == "" {
		jsonError(w, http.StatusBadGateway, "github_oauth_failed", "invalid GitHub device response", nil)
		return
	}
	if device.Interval <= 0 {
		device.Interval = 5
	}
	flowID := token(24)
	a.states.Store("ghdev:"+flowID, ghDeviceFlow{userID: u.ID, deviceCode: device.DeviceCode, interval: device.Interval, expires: time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)})
	jsonResponse(w, http.StatusOK, map[string]any{"device_code": flowID, "user_code": device.UserCode, "verification_uri": device.VerificationURI, "expires_in": device.ExpiresIn, "interval": device.Interval})
}

func (a *application) githubPoll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceCode string `json:"device_code"`
	}
	if !readJSON(w, r, &in) || in.DeviceCode == "" {
		jsonError(w, http.StatusBadRequest, "device_code_required", "device_code is required", nil)
		return
	}
	u, _ := userOf(r)
	v, ok := a.states.Load("ghdev:" + in.DeviceCode)
	flow, ok2 := v.(ghDeviceFlow)
	if !ok || !ok2 || flow.userID != u.ID || time.Now().After(flow.expires) {
		jsonError(w, http.StatusBadRequest, "invalid_device_code", "device code is invalid or expired", nil)
		return
	}
	if a.cfg.GitHub.ClientID == "" {
		jsonError(w, http.StatusServiceUnavailable, "github_not_configured", "github oauth is not configured", nil)
		return
	}
	form := url.Values{"client_id": {a.cfg.GitHub.ClientID}, "client_secret": {a.cfg.GitHub.ClientSecret}, "device_code": {flow.deviceCode}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, a.cfg.GitHub.TokenURL, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		jsonResponse(w, http.StatusOK, map[string]any{"status": "pending", "retry_after": flow.interval})
		return
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if result.Error == "authorization_pending" || result.Error == "slow_down" || result.AccessToken == "" {
		jsonResponse(w, http.StatusOK, map[string]any{"status": "pending", "retry_after": flow.interval})
		return
	}
	login, id, e := a.githubProfile(r.Context(), result.AccessToken)
	if e != nil {
		jsonError(w, http.StatusBadGateway, "github_profile_failed", e.Error(), nil)
		return
	}
	if e := a.saveGitHubGrant(r.Context(), u.ID, id, login, result.AccessToken); e != nil {
		jsonError(w, http.StatusInternalServerError, "github_grant_failed", e.Error(), nil)
		return
	}
	a.states.Delete("ghdev:" + in.DeviceCode)
	a.recordOAuthEvent(r, "github", "device", "success", "")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "completed", "github_login": login})
}

// --- shared helpers ---

func (a *application) githubExchangeCode(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{"client_id": {a.cfg.GitHub.ClientID}, "client_secret": {a.cfg.GitHub.ClientSecret}, "code": {code}, "redirect_uri": {a.cfg.GitHub.RedirectURI}, "code_verifier": {verifier}, "grant_type": {"authorization_code"}}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.GitHub.TokenURL, strings.NewReader(form.Encode()))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if e := json.NewDecoder(resp.Body).Decode(&tok); e != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("token response did not contain access_token")
	}
	return tok.AccessToken, nil
}

func (a *application) githubProfile(ctx context.Context, accessToken string) (string, int64, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(a.cfg.GitHub.APIURL, "/")+"/user", nil)
	if e != nil {
		return "", 0, e
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return "", 0, e
	}
	defer resp.Body.Close()
	var profile struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if e := json.NewDecoder(resp.Body).Decode(&profile); e != nil || profile.ID == 0 {
		return "", 0, fmt.Errorf("invalid GitHub profile response")
	}
	return profile.Login, profile.ID, nil
}

func (a *application) saveGitHubGrant(ctx context.Context, userID string, githubID int64, login, accessToken string) error {
	if a.db == nil {
		return fmt.Errorf("database is unavailable")
	}
	if a.cipher == nil {
		return fmt.Errorf("token encryption is unavailable")
	}
	ciphertext, e := a.cipher.EncryptString(accessToken)
	if e != nil {
		return e
	}
	_, e = a.db.Pool.Exec(ctx, `INSERT INTO github_grants(user_id,github_user_id,login,access_token_cipher) VALUES($1,$2,$3,$4) ON CONFLICT(user_id) DO UPDATE SET github_user_id=EXCLUDED.github_user_id,login=EXCLUDED.login,access_token_cipher=EXCLUDED.access_token_cipher,updated_at=now()`, userID, githubID, login, []byte(ciphertext))
	return e
}

func (a *application) githubRevoke(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	if a.db != nil {
		_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM github_grants WHERE user_id=$1`, u.ID)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *application) recordOAuthEvent(r *http.Request, provider, eventType, result, errCode string) {
	if a.db == nil {
		return
	}
	_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO oauth_events(provider,event_type,result,ip,error_code) VALUES($1,$2,$3,$4,$5)`, provider, eventType, result, clientIP(r), errCode)
}

func (a *application) authSuccess(w http.ResponseWriter, r *http.Request) {
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "授权完成",
		Heading:     "授权完成",
		Description: "可以返回 OronBox 继续使用",
		Tone:        "success",
	})
}

func (a *application) authFailed(w http.ResponseWriter, r *http.Request) {
	errCode := r.URL.Query().Get("error")
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "授权失败",
		Heading:     "授权失败",
		Description: authErrorMessage(errCode),
		Tone:        "danger",
	})
}
