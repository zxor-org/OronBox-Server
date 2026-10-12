package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

type AuthStartResponse struct {
	State        string `json:"state"`
	AuthorizeURL string `json:"authorize_url"`
}

type bandbbsOAuthState struct {
	ExpiresAt time.Time
	Purpose   string
	UserUID   int
}

// HandleAuthStart implements GET/POST /api/v1/auth/bandbbs/start (Spec §5.1)
func (s *Service) HandleAuthStart(w http.ResponseWriter, r *http.Request) {
	purpose := "login"
	boundUID := 0
	if r.Method == http.MethodPost {
		var body struct {
			Purpose string `json:"purpose"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body)
		if strings.EqualFold(strings.TrimSpace(body.Purpose), "publish") {
			purpose = "publish"
			if len(s.Cfg.BandBBS.PublishScopes) == 0 {
				httputil.Error(w, http.StatusServiceUnavailable, "publish_scope_unconfigured", "服务端未配置米坛发布权限")
				return
			}
			boundUID, _ = httputil.AuthenticateUser(r.Context(), s.Store, httputil.ExtractBearerToken(r))
			if boundUID <= 0 {
				httputil.Error(w, http.StatusUnauthorized, "unauthorized", "请先登录 OronBox")
				return
			}
		}
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
	expires := time.Now().Add(ttl)
	if purpose == "publish" {
		s.States.Store(state, bandbbsOAuthState{ExpiresAt: expires, Purpose: purpose, UserUID: boundUID})
	} else {
		s.States.Store(state, expires)
	}
	scope := s.Cfg.BandBBS.Scopes
	if purpose == "publish" && len(s.Cfg.BandBBS.PublishScopes) > 0 {
		scope = strings.Join(s.Cfg.BandBBS.PublishScopes, " ")
	}

	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		s.Cfg.BandBBS.AuthorizeURL,
		url.QueryEscape(s.Cfg.BandBBS.ClientID),
		url.QueryEscape(s.Cfg.BandBBS.RedirectURI),
		url.QueryEscape(scope),
		url.QueryEscape(state),
	)

	// Spec §5.1: 若请求携带 Accept: application/json 则返回 JSON，默认直接 302 重定向
	if strings.Contains(r.Header.Get("Accept"), "application/json") || r.Method == http.MethodPost {
		httputil.JSON(w, http.StatusOK, AuthStartResponse{
			State:        state,
			AuthorizeURL: authURL,
		})
		return
	}
	http.Redirect(w, r, authURL, http.StatusFound)
}

// HandleAuthCallback implements GET /oauth2/bandbbs/callback (Spec §5.2)
func (s *Service) HandleAuthCallback(renderTransition func(w http.ResponseWriter, r *http.Request, data web.TransitionPageData), finishAdminLogin func(w http.ResponseWriter, r *http.Request, code string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		code := r.URL.Query().Get("code")

		if strings.HasPrefix(state, "admin_") {
			realState := strings.TrimPrefix(state, "admin_")
			value, ok := s.States.LoadAndDelete("admin_" + realState)
			if !ok || !stateFresh(value) {
				httputil.Error(w, http.StatusBadRequest, "invalid_state", "管理员授权会话失效")
				return
			}
			if finishAdminLogin != nil {
				finishAdminLogin(w, r, code)
			}
			return
		}

		value, ok := s.States.LoadAndDelete(state)
		if !ok || !stateFresh(value) {
			httputil.Error(w, http.StatusBadRequest, "invalid_state", "授权状态已过期或无效")
			return
		}
		publishState, isPublish := value.(bandbbsOAuthState)

		uid, username, avatar, accessToken, refreshToken, expiresIn, err := s.FetchBandBBSUserWithToken(r.Context(), code, s.Cfg.BandBBS.RedirectURI)
		if err != nil {
			slog.Default().Error("bandbbs oauth callback failed", "request_id", observability.RequestID(r.Context()), "error", observability.SafeError(err))
			httputil.Error(w, http.StatusBadGateway, "oauth_exchange_failed", "第三方授权服务暂时不可用")
			return
		}

		intUID, _ := strconv.Atoi(uid)
		if intUID <= 0 {
			httputil.Error(w, http.StatusInternalServerError, "invalid_uid", "无效的用户UID")
			return
		}
		if isPublish && publishState.UserUID != intUID {
			httputil.Error(w, http.StatusForbidden, "publish_account_mismatch", "发布授权账号与当前账号不一致")
			return
		}

		// Upsert user
		if _, err = s.Store.Pool.Exec(r.Context(), `
			INSERT INTO users (bandbbs_uid, username, avatar_url, updated_at)
			VALUES ($1, $2, $3, now())
			ON CONFLICT (bandbbs_uid) DO UPDATE 
			SET username = EXCLUDED.username, avatar_url = EXCLUDED.avatar_url, updated_at = now()
		`, intUID, username, avatar); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "user_save_failed", "用户信息保存失败")
			return
		}

		// Save grant if cipher available
		if s.Cipher != nil && accessToken != "" {
			grantProvider := "bandbbs"
			grantScopes := strings.Fields(s.Cfg.BandBBS.Scopes)
			if isPublish && publishState.UserUID == intUID {
				grantProvider = "bandbbs_write"
				grantScopes = s.Cfg.BandBBS.PublishScopes
			}
			if encrypted, err := s.Cipher.EncryptString(accessToken); err == nil {
				refreshCipher := []byte{}
				if refreshToken != "" {
					if encryptedRefresh, encErr := s.Cipher.EncryptString(refreshToken); encErr == nil {
						refreshCipher = []byte(encryptedRefresh)
					}
				}
				_, _ = s.Store.Pool.Exec(r.Context(), `
					INSERT INTO oauth_grants (user_uid, provider, subject, scopes, access_token_cipher, refresh_token_cipher, updated_at)
					VALUES ($1, $2, $3, $4, $5, $6, now())
					ON CONFLICT (user_uid, provider) DO UPDATE 
					SET subject = EXCLUDED.subject, access_token_cipher = EXCLUDED.access_token_cipher, refresh_token_cipher = EXCLUDED.refresh_token_cipher, updated_at = now()
				`, intUID, grantProvider, uid, grantScopes, []byte(encrypted), refreshCipher)
				if expiresIn > 0 {
					_, _ = s.Store.Pool.Exec(r.Context(), `UPDATE oauth_grants SET expires_at = now() + ($1 * interval '1 second') WHERE user_uid = $2 AND provider = $3`, expiresIn, intUID, grantProvider)
				}
			}
		}

		// Store only a short-lived ticket. The access/refresh tokens are minted by
		// the atomic ticket exchange endpoint and never travel through a URL.
		ticketBytes := make([]byte, 16)
		if _, err := rand.Read(ticketBytes); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "ticket_generation_failed", "无法生成安全登录票据")
			return
		}
		ticket := hex.EncodeToString(ticketBytes)
		ticketHash := sha256.Sum256([]byte(ticket))
		ticketTTL := s.Cfg.LoginTicketTTL
		if ticketTTL <= 0 {
			ticketTTL = 5 * time.Minute
		}
		if _, err = s.Store.Pool.Exec(r.Context(), `
			INSERT INTO login_tickets (ticket_hash, user_uid, expires_at)
			VALUES ($1, $2, $3)
		`, ticketHash[:], intUID, time.Now().Add(ticketTTL)); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "ticket_failed", "登录票据创建失败")
			return
		}

		deepLink := fmt.Sprintf("%s?ticket=%s&uid=%d&username=%s", s.Cfg.ClientRedirectURI, ticket, intUID, url.QueryEscape(username))
		if renderTransition != nil {
			renderTransition(w, r, web.TransitionPageData{
				Title:       "登录成功",
				Heading:     "登录成功",
				Description: "正在返回 OronBox，若没有自动跳转，请点击下方按钮",
				ButtonLabel: "返回 OronBox",
				Target:      template.URL(deepLink),
				Auto:        true,
				Tone:        "success",
			})
			return
		}
		http.Redirect(w, r, deepLink, http.StatusFound)
	}
}

func stateFresh(value any) bool {
	if state, ok := value.(bandbbsOAuthState); ok {
		return time.Now().Before(state.ExpiresAt)
	}
	expires, ok := value.(time.Time)
	return ok && time.Now().Before(expires)
}

// HandleRevokeSession implements POST /api/v1/auth/session/revoke
func (s *Service) HandleRevokeSession(w http.ResponseWriter, r *http.Request) {
	token := httputil.ExtractBearerToken(r)
	if token == "" {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "缺少 Authorization 令牌")
		return
	}
	hash := sha256.Sum256([]byte(token))
	if _, err := s.Store.Pool.Exec(r.Context(), `
		UPDATE sessions SET revoked_at = now() WHERE access_hash = $1
	`, hash[:]); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "db_error", "会话撤销失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleBandBBSTokenRefresh refreshes the server-held BandBBS grant for the
// authenticated app session. The BandBBS refresh token is decrypted server
// side and never returned to the client.
func (s *Service) HandleBandBBSTokenRefresh(w http.ResponseWriter, r *http.Request) {
	token := httputil.ExtractBearerToken(r)
	userUID, err := httputil.AuthenticateUser(r.Context(), s.Store, token)
	if err != nil || userUID <= 0 || s.Cipher == nil {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或无法刷新米坛令牌")
		return
	}
	var encrypted []byte
	if err := s.Store.Pool.QueryRow(r.Context(), `
		SELECT refresh_token_cipher FROM oauth_grants
		WHERE user_uid = $1 AND provider = 'bandbbs'
	`, userUID).Scan(&encrypted); err != nil || len(encrypted) == 0 {
		httputil.Error(w, http.StatusUnauthorized, "bandbbs_grant_missing", "米坛授权已失效，请重新授权")
		return
	}
	refreshToken, err := s.Cipher.DecryptString(string(encrypted))
	if err != nil || refreshToken == "" {
		httputil.Error(w, http.StatusUnauthorized, "bandbbs_grant_invalid", "米坛授权已失效，请重新授权")
		return
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {s.Cfg.BandBBS.ClientID},
		"client_secret": {s.Cfg.BandBBS.ClientSecret},
		"refresh_token": {refreshToken},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.Cfg.BandBBS.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		httputil.Error(w, http.StatusBadGateway, "bandbbs_refresh_failed", "米坛令牌刷新失败")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := oauthHTTPClient.Do(req)
	if err != nil || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if resp != nil {
			_ = resp.Body.Close()
		}
		httputil.Error(w, http.StatusBadGateway, "bandbbs_refresh_failed", "米坛令牌刷新失败")
		return
	}
	defer resp.Body.Close()
	var refreshed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&refreshed); err != nil || refreshed.AccessToken == "" || refreshed.ExpiresIn <= 0 {
		httputil.Error(w, http.StatusBadGateway, "bandbbs_refresh_failed", "米坛令牌刷新响应无效")
		return
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = refreshToken
	}
	accessCipher, err := s.Cipher.EncryptString(refreshed.AccessToken)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "credential_storage_unavailable", "无法保存米坛令牌")
		return
	}
	refreshCipher, err := s.Cipher.EncryptString(refreshed.RefreshToken)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "credential_storage_unavailable", "无法保存米坛授权")
		return
	}
	if _, err := s.Store.Pool.Exec(r.Context(), `
		UPDATE oauth_grants SET access_token_cipher = $1, refresh_token_cipher = $2, expires_at = now() + ($4 * interval '1 second'), updated_at = now()
		WHERE user_uid = $3 AND provider = 'bandbbs'
	`, []byte(accessCipher), []byte(refreshCipher), userUID, refreshed.ExpiresIn); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "credential_storage_unavailable", "无法保存米坛令牌")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"access_token": refreshed.AccessToken,
		"token_type":   refreshed.TokenType,
		"expires_in":   refreshed.ExpiresIn,
		"scope":        refreshed.Scope,
	})
}
