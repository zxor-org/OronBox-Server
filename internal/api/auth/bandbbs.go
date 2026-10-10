package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

type AuthStartResponse struct {
	State        string `json:"state"`
	AuthorizeURL string `json:"authorize_url"`
}

// HandleAuthStart implements GET/POST /api/v1/auth/bandbbs/start (Spec §5.1)
func (s *Service) HandleAuthStart(w http.ResponseWriter, r *http.Request) {
	stateBytes := make([]byte, 16)
	_, _ = rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)
	expires := time.Now().Add(s.Cfg.StateTTL)
	s.States.Store(state, expires)

	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		s.Cfg.BandBBS.AuthorizeURL,
		url.QueryEscape(s.Cfg.BandBBS.ClientID),
		url.QueryEscape(s.Cfg.BandBBS.RedirectURI),
		url.QueryEscape(s.Cfg.BandBBS.Scopes),
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
			if _, ok := s.States.LoadAndDelete("admin_" + realState); !ok {
				httputil.Error(w, http.StatusBadRequest, "invalid_state", "管理员授权会话失效")
				return
			}
			if finishAdminLogin != nil {
				finishAdminLogin(w, r, code)
			}
			return
		}

		if _, ok := s.States.LoadAndDelete(state); !ok {
			httputil.Error(w, http.StatusBadRequest, "invalid_state", "授权状态已过期或无效")
			return
		}

		uid, username, avatar, accessToken, err := s.FetchBandBBSUserWithToken(r.Context(), code, s.Cfg.BandBBS.RedirectURI)
		if err != nil {
			httputil.Error(w, http.StatusBadGateway, "oauth_exchange_failed", err.Error())
			return
		}

		intUID, _ := strconv.Atoi(uid)
		if intUID <= 0 {
			httputil.Error(w, http.StatusInternalServerError, "invalid_uid", "无效的用户UID")
			return
		}

		// Upsert user
		_, _ = s.Store.Pool.Exec(r.Context(), `
			INSERT INTO users (bandbbs_uid, username, avatar_url, updated_at)
			VALUES ($1, $2, $3, now())
			ON CONFLICT (bandbbs_uid) DO UPDATE 
			SET username = EXCLUDED.username, avatar_url = EXCLUDED.avatar_url, updated_at = now()
		`, intUID, username, avatar)

		// Save grant if cipher available
		if s.Cipher != nil && accessToken != "" {
			if encrypted, err := s.Cipher.EncryptString(accessToken); err == nil {
				_, _ = s.Store.Pool.Exec(r.Context(), `
					INSERT INTO oauth_grants (user_uid, provider, subject, scopes, access_token_cipher, refresh_token_cipher, updated_at)
					VALUES ($1, 'bandbbs', $2, $3, $4, '', now())
					ON CONFLICT (user_uid, provider) DO UPDATE 
					SET subject = EXCLUDED.subject, access_token_cipher = EXCLUDED.access_token_cipher, updated_at = now()
				`, intUID, uid, strings.Fields(s.Cfg.BandBBS.Scopes), []byte(encrypted))
			}
		}

		// Create session token
		tokenBytes := make([]byte, 32)
		_, _ = rand.Read(tokenBytes)
		sessionToken := hex.EncodeToString(tokenBytes)
		tokenHash := sha256.Sum256([]byte(sessionToken))
		refreshHash := sha256.Sum256([]byte(sessionToken + "_refresh"))

		_, err = s.Store.Pool.Exec(r.Context(), `
			INSERT INTO sessions (user_uid, access_hash, refresh_hash, access_expires_at, refresh_expires_at, platform)
			VALUES ($1, $2, $3, now() + interval '30 days', now() + interval '60 days', 'app')
		`, intUID, tokenHash[:], refreshHash[:])

		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "session_failed", err.Error())
			return
		}

		// Bug 2.1: store ticket_hash (SHA256 of ticket) in login_tickets
		ticketBytes := make([]byte, 16)
		_, _ = rand.Read(ticketBytes)
		ticket := hex.EncodeToString(ticketBytes)
		ticketHash := sha256.Sum256([]byte(ticket))
		_, _ = s.Store.Pool.Exec(r.Context(), `
			INSERT INTO login_tickets (ticket_hash, user_uid, expires_at)
			VALUES ($1, $2, now() + interval '5 minutes')
		`, ticketHash[:], intUID)

		deepLink := fmt.Sprintf("%s?ticket=%s&token=%s&uid=%d&username=%s", s.Cfg.ClientRedirectURI, ticket, sessionToken, intUID, url.QueryEscape(username))
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

// HandleRevokeSession implements POST /api/v1/auth/session/revoke
func (s *Service) HandleRevokeSession(w http.ResponseWriter, r *http.Request) {
	token := httputil.ExtractBearerToken(r)
	if token != "" {
		hash := sha256.Sum256([]byte(token))
		_, _ = s.Store.Pool.Exec(r.Context(), `
			UPDATE sessions SET revoked_at = now() WHERE access_hash = $1
		`, hash[:])
	}
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
