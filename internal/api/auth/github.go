package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/observability"
)

// HandleGithubWebStart implements GET /api/v1/auth/github/web/start (Spec §7.1)
func (s *Service) HandleGithubWebStart(w http.ResponseWriter, r *http.Request) {
	token := httputil.ExtractBearerToken(r)
	userUID, err := httputil.AuthenticateUser(r.Context(), s.Store, token)
	if err != nil || userUID <= 0 {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
		return
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "state_generation_failed", "无法生成安全授权状态")
		return
	}
	state := hex.EncodeToString(stateBytes)
	ttl := s.Cfg.StateTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	s.States.Store("gh_"+state, githubState{UserUID: userUID, ExpiresAt: time.Now().Add(ttl)})

	authURL := fmt.Sprintf("https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=read:user&state=%s",
		url.QueryEscape(s.Cfg.GitHub.ClientID),
		url.QueryEscape(s.Cfg.GitHub.RedirectURI),
		url.QueryEscape(state),
	)

	httputil.JSON(w, http.StatusOK, map[string]string{
		"state":         state,
		"authorize_url": authURL,
	})
}

// HandleGithubWebStatus implements GET /api/v1/auth/github/grant (Spec §7.2)
func (s *Service) HandleGithubWebStatus(w http.ResponseWriter, r *http.Request) {
	token := httputil.ExtractBearerToken(r)
	userUID, err := httputil.AuthenticateUser(r.Context(), s.Store, token)
	if err != nil || userUID <= 0 {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
		return
	}

	var login string
	var updatedAt time.Time
	err = s.Store.Pool.QueryRow(r.Context(), `
		SELECT login, updated_at FROM github_grants WHERE user_uid = $1
	`, userUID).Scan(&login, &updatedAt)

	if err != nil || login == "" {
		httputil.JSON(w, http.StatusOK, map[string]any{"bound": false})
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{
		"bound":      true,
		"login":      login,
		"updated_at": updatedAt.Format(time.RFC3339),
	})
}

// HandleGithubWebCallback implements GET /oauth2/github/callback (Spec §7.3)
func (s *Service) HandleGithubWebCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	val, ok := s.States.LoadAndDelete("gh_" + state)
	if !ok {
		httputil.Error(w, http.StatusBadRequest, "invalid_state", "无效的绑定会话")
		return
	}
	entry, ok := val.(githubState)
	if !ok || !time.Now().Before(entry.ExpiresAt) {
		httputil.Error(w, http.StatusBadRequest, "invalid_state", "绑定会话已过期")
		return
	}
	uid := entry.UserUID

	token, err := s.exchangeGitHubCode(r.Context(), code)
	if err != nil {
		slog.Default().Error("github oauth exchange failed", "request_id", observability.RequestID(r.Context()), "error", observability.SafeError(err))
		httputil.Error(w, http.StatusBadGateway, "exchange_failed", "第三方授权服务暂时不可用")
		return
	}

	login, ghID, err := s.fetchGitHubProfile(r.Context(), token)
	if err != nil {
		slog.Default().Error("github oauth profile failed", "request_id", observability.RequestID(r.Context()), "error", observability.SafeError(err))
		httputil.Error(w, http.StatusBadGateway, "profile_failed", "第三方用户信息获取失败")
		return
	}

	if s.Cipher == nil || token == "" {
		httputil.Error(w, http.StatusInternalServerError, "credential_storage_unavailable", "无法安全保存 GitHub 凭据")
		return
	}
	cipherBytes, err := s.Cipher.Encrypt([]byte(token))
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "credential_encryption_failed", "无法安全保存 GitHub 凭据")
		return
	}

	_, err = s.Store.Pool.Exec(r.Context(), `
		INSERT INTO github_grants (user_uid, github_user_id, login, access_token_cipher, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (user_uid) DO UPDATE 
		SET github_user_id = EXCLUDED.github_user_id, login = EXCLUDED.login, 
		    access_token_cipher = EXCLUDED.access_token_cipher, updated_at = now()
	`, uid, ghID, login, cipherBytes)

	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	httputil.JSON(w, http.StatusOK, map[string]any{"status": "bound", "login": login})
}

type githubState struct {
	UserUID   int
	ExpiresAt time.Time
}

func (s githubState) Expiry() time.Time { return s.ExpiresAt }

// HandleGithubRevoke implements POST/DELETE /api/v1/auth/github/grant
func (s *Service) HandleGithubRevoke(w http.ResponseWriter, r *http.Request) {
	token := httputil.ExtractBearerToken(r)
	userUID, err := httputil.AuthenticateUser(r.Context(), s.Store, token)
	if err != nil || userUID <= 0 {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}

	_, _ = s.Store.Pool.Exec(r.Context(), `DELETE FROM github_grants WHERE user_uid = $1`, userUID)
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "unbound"})
}

func (s *Service) exchangeGitHubCode(ctx context.Context, code string) (string, error) {
	data := url.Values{
		"client_id":     {s.Cfg.GitHub.ClientID},
		"client_secret": {s.Cfg.GitHub.ClientSecret},
		"code":          {code},
		"redirect_uri":  {s.Cfg.GitHub.RedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", s.Cfg.GitHub.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("github token exchange failed")
	}
	return out.AccessToken, nil
}

func (s *Service) fetchGitHubProfile(ctx context.Context, token string) (string, int64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.Cfg.GitHub.APIURL+"/user", nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	var out struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ID <= 0 {
		return "", 0, fmt.Errorf("fetch github profile failed")
	}
	return out.Login, out.ID, nil
}
