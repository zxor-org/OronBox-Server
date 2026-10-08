package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/attestation"
	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

type application struct {
	cfg     config.Config
	db      *store.Store
	started time.Time
	cipher  *auth.Cipher
	states  sync.Map
	limits  *rateLimiter
	nonces  *attestation.NonceStore
	revMu       sync.RWMutex
	revoked     attestation.Revocation
	minimum     string
	adminAuthFn func(*http.Request) bool
}
type userContext struct {
	ID                        string
	BandBBSUID                int64
	Username, AvatarURL, Role string
	Banned                    bool
	CreatedAt                 time.Time
}
type ctxKey string

func newApplication(d Dependencies) *application {
	c, _ := auth.NewCipher(d.Config.TokenEncryptionKey)
	return &application{cfg: d.Config, db: d.Store, started: d.StartedAt, cipher: c, limits: newRateLimiter(), nonces: attestation.NewNonceStore(d.Config.ClientAttestationSkew), revoked: attestation.NewRevocation(nil)}
}
func (a *application) withAttestation(next http.Handler) http.Handler {
	if !a.cfg.ClientAttestationEnabled {
		return next
	}
	a.revMu.RLock()
	rev, min := a.revoked, a.minimum
	a.revMu.RUnlock()
	return attestation.Middleware{Options: attestation.Options{
		MasterKey: a.cfg.AttestationMasterKey, NonceStore: a.nonces,
		Skew: a.cfg.ClientAttestationSkew, Revocation: rev, MinimumVersion: min,
	}, Next: next}
}

// refreshRevocation reloads the two-level revocation policy from server_settings.
func (a *application) refreshRevocation() {
	if a.db == nil {
		return
	}
	rev := attestation.NewRevocation(nil)
	if raw, err := a.settingRaw("auth_revoked_keys"); err == nil && len(raw) > 0 {
		var fps []string
		if json.Unmarshal(raw, &fps) == nil {
			rev = attestation.NewRevocation(fps)
		} else {
			var objs []struct {
				Fingerprint string `json:"fingerprint"`
			}
			if json.Unmarshal(raw, &objs) == nil {
				for _, o := range objs {
					fps = append(fps, o.Fingerprint)
				}
				rev = attestation.NewRevocation(fps)
			}
		}
	}
	min := ""
	if raw, err := a.settingRaw("auth_minimum_version"); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &min)
	}
	a.revMu.Lock()
	a.revoked, a.minimum = rev, min
	a.revMu.Unlock()
}

func (a *application) settingRaw(key string) ([]byte, error) {
	var raw []byte
	err := a.db.Pool.QueryRow(context.Background(), `SELECT value FROM server_settings WHERE key=$1`, key).Scan(&raw)
	return raw, err
}
func (a *application) requireUser(r *http.Request) (userContext, error) {
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token == "" || a.db == nil {
		return userContext{}, fmt.Errorf("missing bearer token")
	}
	h := sha256.Sum256([]byte(token))
	var u userContext
	err := a.db.Pool.QueryRow(r.Context(), `SELECT u.id,u.bandbbs_uid,u.username,COALESCE(u.avatar_url,''),u.role,u.banned,u.created_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.access_hash=$1 AND s.access_expires_at>now() AND s.revoked_at IS NULL`, h[:]).Scan(&u.ID, &u.BandBBSUID, &u.Username, &u.AvatarURL, &u.Role, &u.Banned, &u.CreatedAt)
	return u, err
}
func authRequired(a *application, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, e := a.requireUser(r)
		if e != nil {
			jsonError(w, 401, "unauthorized", "valid bearer token required", nil)
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey("user"), u)))
	})
}
func userOf(r *http.Request) (userContext, bool) {
	u, ok := r.Context().Value(ctxKey("user")).(userContext)
	return u, ok
}
func (a *application) authStart(w http.ResponseWriter, r *http.Request) {
	state := token(24)
	expires := time.Now().Add(a.cfg.StateTTL)
	a.states.Store(state, expires)

	appID := "oronbox"
	platform := "unknown"
	returnURI := a.cfg.ClientRedirectURI

	if r.Method == http.MethodPost {
		var req struct {
			AppID     string `json:"app_id"`
			Platform  string `json:"platform"`
			ReturnURI string `json:"return_uri"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			if req.AppID != "" {
				appID = req.AppID
			}
			if req.Platform != "" {
				platform = req.Platform
			}
			if req.ReturnURI != "" {
				returnURI = req.ReturnURI
			}
		}
	} else {
		if v := r.URL.Query().Get("app_id"); v != "" {
			appID = v
		}
		if v := r.URL.Query().Get("platform"); v != "" {
			platform = v
		}
		if v := r.URL.Query().Get("return_uri"); v != "" {
			returnURI = v
		}
	}

	if a.db != nil {
		if _, err := a.db.Pool.Exec(r.Context(), `INSERT INTO oauth_states(id,provider,purpose,expires_at,app_id,platform,return_uri,ip,user_agent) VALUES($1,'bandbbs','login',$2,$3,$4,$5,$6,$7)`, state, expires, appID, platform, returnURI, r.RemoteAddr, r.UserAgent()); err != nil {
			jsonError(w, 500, "oauth_state_create_failed", err.Error(), nil)
			return
		}
	}
	scope := a.cfg.BandBBS.Scopes
	if strings.TrimSpace(scope) == "" {
		scope = "user:read"
	}
	target := a.cfg.BandBBS.AuthorizeURL + "?response_type=code&client_id=" + escape(a.cfg.BandBBS.ClientID) + "&state=" + state + "&redirect_uri=" + escape(a.cfg.BandBBS.RedirectURI) + "&scope=" + escape(scope)

	if r.Method == http.MethodPost || r.Header.Get("Accept") == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"authorization_url": target,
		})
		return
	}
	http.Redirect(w, r, target, 302)
}
func (a *application) authCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if v, ok := a.states.LoadAndDelete("admin:" + state); ok {
		a.finishAdminLogin(w, r, v)
		return
	}
	if v, ok := a.states.LoadAndDelete("publish:" + state); ok {
		a.finishPublishGrant(w, r, v)
		return
	}
	v, ok := a.states.LoadAndDelete(state)
	if !ok {
		jsonError(w, 400, "invalid_state", "OAuth state is invalid or expired", nil)
		return
	}
	if expires, ok := v.(time.Time); !ok || time.Now().After(expires) {
		jsonError(w, 400, "invalid_state", "OAuth state is invalid or expired", nil)
		return
	}
	uid, username, avatar, accessToken, e := a.fetchBandBBSUserWithToken(r.Context(), r.URL.Query().Get("code"), a.cfg.BandBBS.RedirectURI)
	if e != nil {
		jsonError(w, 502, "oauth_exchange_failed", e.Error(), nil)
		return
	}
	if a.db == nil {
		jsonError(w, 503, "database_unavailable", "database is unavailable", nil)
		return
	}
	userID, e := a.upsertUser(r.Context(), uid, username, avatar)
	if e != nil {
		jsonError(w, 500, "user_create_failed", e.Error(), nil)
		return
	}
	if a.cipher != nil {
		if encrypted, encErr := a.cipher.EncryptString(accessToken); encErr == nil {
			_, _ = a.db.Pool.Exec(r.Context(), `INSERT INTO oauth_grants(user_id,provider,subject,scopes,access_token_cipher) VALUES($1,'bandbbs',$2,$3,$4) ON CONFLICT(user_id,provider) DO UPDATE SET subject=EXCLUDED.subject,access_token_cipher=EXCLUDED.access_token_cipher,updated_at=now()`, userID, uid, strings.Fields(a.cfg.BandBBS.Scopes), []byte(encrypted))
		}
	}
	ticket := token(32)
	if _, e = a.db.Pool.Exec(r.Context(), `INSERT INTO login_tickets(ticket_hash,user_id,expires_at) VALUES($1,$2,now()+$3::interval)`, hash(ticket), userID, a.cfg.LoginTicketTTL.String()); e != nil {
		jsonError(w, 500, "ticket_create_failed", e.Error(), nil)
		return
	}
	deepLink := a.cfg.ClientRedirectURI + "?ticket=" + escape(ticket)
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "登录成功",
		Heading:     "登录成功",
		Description: "正在返回 OronBox，若没有自动跳转，请点击下方按钮",
		ButtonLabel: "返回 OronBox",
		Target:      template.URL(deepLink),
		Auto:        true,
		Tone:        "success",
	})
}

type publishState struct {
	UserID  string
	Expires time.Time
}

// publishStart begins the incremental BandBBS authorization for publishing
// (provider=bandbbs_publish). The client opens the returned URL in a browser.
func (a *application) publishStart(w http.ResponseWriter, r *http.Request) {
	u, ok := userOf(r)
	if !ok {
		jsonError(w, 401, "unauthorized", "authentication required", nil)
		return
	}
	state := token(24)
	a.states.Store("publish:"+state, publishState{UserID: u.ID, Expires: time.Now().Add(a.cfg.StateTTL)})
	scope := strings.Join(a.cfg.BandBBS.PublishScopes, " ")
	target := a.cfg.BandBBS.AuthorizeURL + "?response_type=code&client_id=" + escape(a.cfg.BandBBS.ClientID) +
		"&state=" + escape(state) + "&redirect_uri=" + escape(a.cfg.BandBBS.RedirectURI) + "&scope=" + escape(scope)
	jsonResponse(w, 200, map[string]string{"authorization_url": target})
}

func (a *application) finishPublishGrant(w http.ResponseWriter, r *http.Request, v any) {
	ps, ok := v.(publishState)
	if !ok || time.Now().After(ps.Expires) {
		jsonError(w, 400, "invalid_state", "OAuth state is invalid or expired", nil)
		return
	}
	tok, e := a.exchangeBandBBSToken(r.Context(), r.URL.Query().Get("code"), a.cfg.BandBBS.RedirectURI)
	if e != nil {
		a.recordOAuthEvent(r, "bandbbs", "publish_grant", "failure", "oauth_exchange_failed")
		jsonError(w, 502, "oauth_exchange_failed", e.Error(), nil)
		return
	}
	if trimmed := strings.TrimSpace(tok.Scope); trimmed != "" && !hasScopes(trimmed, a.cfg.BandBBS.PublishScopes) {
		jsonError(w, 403, "insufficient_scope", "BandBBS publishing scope was not granted", nil)
		return
	}
	uid, _, _, e := a.bandbbsIdentity(r.Context(), tok.AccessToken)
	if e != nil {
		jsonError(w, 502, "oauth_identity_failed", e.Error(), nil)
		return
	}
	if a.cipher == nil || a.db == nil {
		jsonError(w, 503, "unavailable", "server is not ready", nil)
		return
	}
	access, e1 := a.cipher.EncryptString(tok.AccessToken)
	if e1 != nil {
		jsonError(w, 500, "encrypt_failed", "token encryption failed", nil)
		return
	}
	var refresh []byte
	if strings.TrimSpace(tok.RefreshToken) != "" {
		enc, e2 := a.cipher.EncryptString(tok.RefreshToken)
		if e2 != nil {
			jsonError(w, 500, "encrypt_failed", "token encryption failed", nil)
			return
		}
		refresh = []byte(enc)
	}
	var expires any
	if tok.ExpiresIn > 0 {
		expires = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	}
	if _, e := a.db.Pool.Exec(r.Context(), `INSERT INTO oauth_grants(user_id,provider,subject,scopes,access_token_cipher,refresh_token_cipher,expires_at) VALUES($1,'bandbbs_publish',$2,$3,$4,$5,$6) ON CONFLICT(user_id,provider) DO UPDATE SET subject=EXCLUDED.subject,scopes=EXCLUDED.scopes,access_token_cipher=EXCLUDED.access_token_cipher,refresh_token_cipher=EXCLUDED.refresh_token_cipher,expires_at=EXCLUDED.expires_at,updated_at=now()`, ps.UserID, uid, a.cfg.BandBBS.PublishScopes, []byte(access), refresh, expires); e != nil {
		jsonError(w, 500, "grant_save_failed", e.Error(), nil)
		return
	}
	a.recordOAuthEvent(r, "bandbbs", "publish_grant", "success", "")
	deepLink := a.cfg.ClientRedirectURI + "?publish=ok"
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "发布授权完成",
		Heading:     "发布授权完成",
		Description: "正在返回 OronBox，若没有自动跳转，请点击下方按钮",
		ButtonLabel: "返回 OronBox",
		Target:      template.URL(deepLink),
		Auto:        true,
		Tone:        "success",
	})
}

type bandbbsTokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (a *application) exchangeBandBBSToken(ctx context.Context, code, redirect string) (bandbbsTokenPayload, error) {
	var tok bandbbsTokenPayload
	if code == "" {
		return tok, fmt.Errorf("missing authorization code")
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {a.cfg.BandBBS.ClientID}, "client_secret": {a.cfg.BandBBS.ClientSecret}, "redirect_uri": {redirect}}
	req, e := http.NewRequestWithContext(ctx, "POST", a.cfg.BandBBS.TokenURL, strings.NewReader(form.Encode()))
	if e != nil {
		return tok, e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		return tok, e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return tok, fmt.Errorf("token endpoint returned %s", resp.Status)
	}
	if e := json.NewDecoder(resp.Body).Decode(&tok); e != nil {
		return tok, e
	}
	if tok.AccessToken == "" {
		return tok, fmt.Errorf("token response did not contain access_token")
	}
	return tok, nil
}

func parseBandBBSProfile(r io.Reader) (uid, username, avatar string, err error) {
	var body map[string]any
	if err := json.NewDecoder(r).Decode(&body); err != nil {
		return "", "", "", err
	}
	data := body
	if meMap, ok := body["me"].(map[string]any); ok {
		data = meMap
	} else if userMap, ok := body["user"].(map[string]any); ok {
		data = userMap
	}

	for _, k := range []string{"user_id", "uid", "id"} {
		if val, exists := data[k]; exists && val != nil {
			switch v := val.(type) {
			case float64:
				uid = strconv.FormatInt(int64(v), 10)
			case int64:
				uid = strconv.FormatInt(v, 10)
			case int:
				uid = strconv.Itoa(v)
			case string:
				uid = strings.TrimSpace(v)
			default:
				uid = fmt.Sprint(v)
			}
			if uid != "" && uid != "<nil>" && uid != "0" {
				break
			}
		}
	}

	if nameVal, ok := data["username"]; ok && nameVal != nil {
		username = fmt.Sprint(nameVal)
	} else if nameVal, ok := data["name"]; ok && nameVal != nil {
		username = fmt.Sprint(nameVal)
	}

	if avMap, ok := data["avatar_urls"].(map[string]any); ok {
		for _, sizeKey := range []string{"o", "l", "m", "s"} {
			if u, ok := avMap[sizeKey].(string); ok && u != "" {
				avatar = u
				break
			}
		}
	} else if avStr, ok := data["avatar_url"].(string); ok {
		avatar = avStr
	}

	if uid == "" || uid == "<nil>" || uid == "0" {
		return "", "", "", errors.New("BandBBS identity response missing valid user_id")
	}
	return uid, username, avatar, nil
}

func (a *application) bandbbsIdentity(ctx context.Context, accessToken string) (uid, username, avatar string, err error) {
	me, e := http.NewRequestWithContext(ctx, "GET", a.cfg.BandBBS.MeURL, nil)
	if e != nil {
		return "", "", "", e
	}
	me.Header.Set("Authorization", "Bearer "+accessToken)
	r, e := http.DefaultClient.Do(me)
	if e != nil {
		return "", "", "", e
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("BandBBS me endpoint returned %s", r.Status)
	}
	return parseBandBBSProfile(r.Body)
}

func hasScopes(granted string, required []string) bool {
	have := map[string]bool{}
	for _, s := range strings.Fields(granted) {
		have[s] = true
	}
	for _, s := range required {
		if !have[s] {
			return false
		}
	}
	return true
}
func (a *application) adminAuthorize(w http.ResponseWriter, r *http.Request) {
	state := token(24)
	a.states.Store("admin:"+state, time.Now().Add(a.cfg.StateTTL))
	target := a.cfg.BandBBS.AuthorizeURL + "?response_type=code&client_id=" + escape(a.cfg.BandBBS.ClientID) + "&state=" + escape(state) + "&redirect_uri=" + escape(a.cfg.BandBBS.RedirectURI) + "&scope=user:read"
	http.Redirect(w, r, target, 302)
}
func (a *application) finishAdminLogin(w http.ResponseWriter, r *http.Request, v any) {
	if expires, ok := v.(time.Time); !ok || time.Now().After(expires) {
		jsonError(w, 400, "invalid_state", "OAuth state is invalid or expired", nil)
		return
	}
	uid, username, avatar, accessToken, e := a.fetchBandBBSUserWithToken(r.Context(), r.URL.Query().Get("code"), a.cfg.BandBBS.RedirectURI)
	if e != nil {
		jsonError(w, 502, "oauth_exchange_failed", e.Error(), nil)
		return
	}
	if accessToken != "" && a.cfg.BandBBS.RevokeURL != "" {
		go func(tok string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = a.revokeBandBBSToken(ctx, tok)
		}(accessToken)
	}
	allowed := false
	for _, v := range a.cfg.AdminBandBBSUIDs {
		if v == uid {
			allowed = true
		}
	}
	if !allowed {
		a.recordOAuthEvent(r, "bandbbs", "admin_login", "failure", "admin_access_denied")
		jsonError(w, 403, "admin_access_denied", "account is not an authorized administrator", nil)
		return
	}
	if a.db == nil {
		jsonError(w, 503, "database_unavailable", "database is unavailable", nil)
		return
	}
	userID, e := a.upsertUser(r.Context(), uid, username, avatar)
	if e != nil {
		jsonError(w, 500, "admin_user_failed", e.Error(), nil)
		return
	}
	session := token(32)
	h := sha256.Sum256([]byte(session))
	// 每个管理员账号限制单一活跃会话：新登录强制注销该账号的历史旧会话
	_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM admin_sessions WHERE user_id=$1`, userID)
	if _, e := a.db.Pool.Exec(r.Context(), `INSERT INTO admin_sessions(session_hash,user_id,username,ip,expires_at) VALUES($1,$2,$3,$4,now()+$5::interval)`, h[:], userID, username, r.RemoteAddr, (24 * time.Hour).String()); e != nil {
		jsonError(w, 500, "admin_session_failed", e.Error(), nil)
		return
	}
	a.setAdminCookie(w, session)
	a.recordOAuthEvent(r, "bandbbs", "admin_login", "success", "")
	http.Redirect(w, r, "/console/", 302)
}
func (a *application) setAdminCookie(w http.ResponseWriter, session string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "oronbox_admin",
		Value:    session,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cfg.PublicURL != "" && strings.HasPrefix(a.cfg.PublicURL, "https://"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   86400,
	})
}
func (a *application) upsertUser(ctx context.Context, uid, username, avatar string) (string, error) {
	n, e := strconv.ParseInt(uid, 10, 64)
	if e != nil {
		return "", fmt.Errorf("invalid BandBBS uid: %w", e)
	}
	var id string
	e = a.db.Pool.QueryRow(ctx, `INSERT INTO users(bandbbs_uid,username,avatar_url) VALUES($1,$2,$3) ON CONFLICT(bandbbs_uid) DO UPDATE SET username=EXCLUDED.username,avatar_url=EXCLUDED.avatar_url RETURNING id`, n, username, avatar).Scan(&id)
	return id, e
}
func (a *application) fetchBandBBSUserWithToken(ctx context.Context, code, redirect string) (string, string, string, string, error) {
	tok, err := a.exchangeBandBBSToken(ctx, code, redirect)
	if err != nil {
		return "", "", "", "", err
	}
	uid, username, avatar, err := a.bandbbsIdentity(ctx, tok.AccessToken)
	if err != nil {
		return "", "", "", "", err
	}
	return uid, username, avatar, tok.AccessToken, nil
}
func (a *application) revokeBandBBSToken(ctx context.Context, token string) error {
	if token == "" || a.cfg.BandBBS.RevokeURL == "" {
		return nil
	}
	form := url.Values{
		"client_id":       {a.cfg.BandBBS.ClientID},
		"client_secret":   {a.cfg.BandBBS.ClientSecret},
		"token":           {token},
		"token_type_hint": {"access_token"},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", a.cfg.BandBBS.RevokeURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
func (a *application) isAdminAuthenticated(r *http.Request) bool {
	if a.adminAuthFn != nil {
		return a.adminAuthFn(r)
	}
	cookie, e := r.Cookie("oronbox_admin")
	if e != nil || cookie.Value == "" || a.db == nil {
		return false
	}
	h := sha256.Sum256([]byte(cookie.Value))
	var uid string
	var expiresAt time.Time
	if e = a.db.Pool.QueryRow(r.Context(), `SELECT user_id, expires_at FROM admin_sessions WHERE session_hash=$1 AND expires_at>now()`, h[:]).Scan(&uid, &expiresAt); e != nil {
		return false
	}
	return true
}
func (a *application) adminProtected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, e := r.Cookie("oronbox_admin")
		if e != nil || cookie.Value == "" || a.db == nil {
			jsonError(w, 401, "admin_unauthorized", "administrator session required", nil)
			return
		}
		h := sha256.Sum256([]byte(cookie.Value))
		var uid string
		var expiresAt time.Time
		if e = a.db.Pool.QueryRow(r.Context(), `SELECT user_id, expires_at FROM admin_sessions WHERE session_hash=$1 AND expires_at>now()`, h[:]).Scan(&uid, &expiresAt); e != nil {
			jsonError(w, 401, "admin_unauthorized", "administrator session required", nil)
			return
		}
		if time.Until(expiresAt) < 12*time.Hour {
			if _, err := a.db.Pool.Exec(r.Context(), `UPDATE admin_sessions SET expires_at=now()+$1::interval WHERE session_hash=$2`, (24 * time.Hour).String(), h[:]); err == nil {
				a.setAdminCookie(w, cookie.Value)
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (a *application) adminLogout(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("oronbox_admin"); e == nil && a.db != nil {
		h := sha256.Sum256([]byte(c.Value))
		_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM admin_sessions WHERE session_hash=$1`, h[:])
	}
	http.SetCookie(w, &http.Cookie{Name: "oronbox_admin", MaxAge: -1, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	w.WriteHeader(204)
}
func (a *application) adminSession(w http.ResponseWriter, r *http.Request) {
	if c, e := r.Cookie("oronbox_admin"); e != nil || a.db == nil {
		jsonResponse(w, 200, map[string]bool{"authenticated": false})
		return
	} else {
		h := sha256.Sum256([]byte(c.Value))
		var uid int64
		var username, avatar string
		var expiresAt time.Time
		if e = a.db.Pool.QueryRow(r.Context(), `SELECT u.bandbbs_uid,u.username,COALESCE(u.avatar_url,''),s.expires_at FROM admin_sessions s JOIN users u ON u.id=s.user_id WHERE s.session_hash=$1 AND s.expires_at>now()`, h[:]).Scan(&uid, &username, &avatar, &expiresAt); e == nil {
			if time.Until(expiresAt) < 12*time.Hour {
				if _, err := a.db.Pool.Exec(r.Context(), `UPDATE admin_sessions SET expires_at=now()+$1::interval WHERE session_hash=$2`, (24 * time.Hour).String(), h[:]); err == nil {
					a.setAdminCookie(w, c.Value)
				}
			}
			jsonResponse(w, 200, map[string]any{"authenticated": true, "bandbbs_uid": uid, "username": username, "avatar_url": avatar, "expires_in": int(time.Until(expiresAt).Seconds())})
			return
		}
	}
	jsonResponse(w, 200, map[string]bool{"authenticated": false})
}
func (a *application) adminRefresh(w http.ResponseWriter, r *http.Request) {
	cookie, e := r.Cookie("oronbox_admin")
	if e != nil || cookie.Value == "" || a.db == nil {
		jsonError(w, 401, "admin_unauthorized", "administrator session required", nil)
		return
	}
	oldHash := sha256.Sum256([]byte(cookie.Value))
	newSession := token(32)
	newHash := sha256.Sum256([]byte(newSession))
	res, err := a.db.Pool.Exec(r.Context(), `UPDATE admin_sessions SET session_hash=$1, expires_at=now()+$2::interval WHERE session_hash=$3 AND expires_at>now()`, newHash[:], (24 * time.Hour).String(), oldHash[:])
	if err != nil || res.RowsAffected() == 0 {
		jsonError(w, 401, "admin_unauthorized", "administrator session expired or invalid", nil)
		return
	}
	a.setAdminCookie(w, newSession)
	jsonResponse(w, 200, map[string]any{"refreshed": true})
}
func (a *application) exchange(w http.ResponseWriter, r *http.Request) {
	if !a.limits.Allow("exchange:"+clientIP(r), 10, time.Minute) {
		jsonError(w, http.StatusTooManyRequests, "rate_limited", "too many exchange attempts", nil)
		return
	}
	var in struct {
		Ticket     string `json:"ticket"`
		Platform   string `json:"platform"`
		AppID      string `json:"app_id"`
		AppVersion string `json:"app_version"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if a.db == nil {
		jsonError(w, 503, "database_unavailable", "database is unavailable", nil)
		return
	}
	access, refresh := token(32), token(32)
	ah, rh := sha256.Sum256([]byte(access)), sha256.Sum256([]byte(refresh))
	var e error
	e = a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		var uid string
		if err := tx.QueryRow(r.Context(), `SELECT user_id FROM login_tickets WHERE ticket_hash=$1 AND used_at IS NULL AND expires_at>now() FOR UPDATE`, hash(in.Ticket)).Scan(&uid); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE login_tickets SET used_at=now() WHERE ticket_hash=$1`, hash(in.Ticket)); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO sessions(user_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,platform) VALUES($1,$2,$3,now()+$4::interval,now()+$5::interval,$6)`, uid, ah[:], rh[:], a.cfg.AccessTokenTTL.String(), a.cfg.RefreshTokenTTL.String(), in.Platform)
		return err
	})
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			a.recordOAuthEvent(r, "bandbbs", "exchange", "failure", "invalid_ticket")
			jsonError(w, 401, "invalid_ticket", "ticket is invalid or expired", nil)
			return
		}
		a.recordOAuthEvent(r, "bandbbs", "exchange", "failure", "session_create_failed")
		jsonError(w, 500, "session_create_failed", e.Error(), nil)
		return
	}
	var u userContext
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT u.id,u.bandbbs_uid,u.username,COALESCE(u.avatar_url,''),u.role,u.banned,u.created_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.access_hash=$1 AND s.access_expires_at>now() AND s.revoked_at IS NULL`, ah[:]).Scan(&u.ID, &u.BandBBSUID, &u.Username, &u.AvatarURL, &u.Role, &u.Banned, &u.CreatedAt); err == nil {
		a.recordOAuthEvent(r, "bandbbs", "exchange", "success", "")
		jsonResponse(w, 200, map[string]any{
			"token_type":    "Bearer",
			"access_token":  access,
			"refresh_token": refresh,
			"expires_in":    int(a.cfg.AccessTokenTTL.Seconds()),
			"user": map[string]any{
				"id":           u.ID,
				"bandbbs_uid":  u.BandBBSUID,
				"username":     u.Username,
				"avatar_url":   u.AvatarURL,
				"role":         u.Role,
				"banned":       u.Banned,
				"created_at":   u.CreatedAt,
			},
		})
		return
	}
	a.recordOAuthEvent(r, "bandbbs", "exchange", "success", "")
	jsonResponse(w, 200, map[string]any{"token_type": "Bearer", "access_token": access, "refresh_token": refresh, "expires_in": int(a.cfg.AccessTokenTTL.Seconds())})
}
func (a *application) refresh(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RefreshToken string `json:"refresh_token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if a.db == nil {
		jsonError(w, 503, "database_unavailable", "database is unavailable", nil)
		return
	}
	access, refresh := token(32), token(32)
	ah, rh := sha256.Sum256([]byte(access)), sha256.Sum256([]byte(refresh))
	e := a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		var sid string
		if err := tx.QueryRow(r.Context(), `SELECT id FROM sessions WHERE refresh_hash=$1 AND refresh_expires_at>now() AND revoked_at IS NULL FOR UPDATE`, hash(in.RefreshToken)).Scan(&sid); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `UPDATE sessions SET refresh_hash=$1,access_hash=$2,access_expires_at=now()+$3::interval,last_seen_at=now() WHERE id=$4`, rh[:], ah[:], a.cfg.AccessTokenTTL.String(), sid)
		return err
	})
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			a.recordOAuthEvent(r, "bandbbs", "refresh", "failure", "invalid_refresh_token")
			jsonError(w, 401, "invalid_refresh_token", "refresh token is invalid or expired", nil)
			return
		}
		a.recordOAuthEvent(r, "bandbbs", "refresh", "failure", "session_refresh_failed")
		jsonError(w, 500, "session_refresh_failed", e.Error(), nil)
		return
	}
	var u userContext
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT u.id,u.bandbbs_uid,u.username,COALESCE(u.avatar_url,''),u.role,u.banned,u.created_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.access_hash=$1 AND s.access_expires_at>now() AND s.revoked_at IS NULL`, ah[:]).Scan(&u.ID, &u.BandBBSUID, &u.Username, &u.AvatarURL, &u.Role, &u.Banned, &u.CreatedAt); err == nil {
		a.recordOAuthEvent(r, "bandbbs", "refresh", "success", "")
		jsonResponse(w, 200, map[string]any{
			"token_type":    "Bearer",
			"access_token":  access,
			"refresh_token": refresh,
			"expires_in":    int(a.cfg.AccessTokenTTL.Seconds()),
			"user": map[string]any{
				"id":           u.ID,
				"bandbbs_uid":  u.BandBBSUID,
				"username":     u.Username,
				"avatar_url":   u.AvatarURL,
				"role":         u.Role,
				"banned":       u.Banned,
				"created_at":   u.CreatedAt,
			},
		})
		return
	}
	a.recordOAuthEvent(r, "bandbbs", "refresh", "success", "")
	jsonResponse(w, 200, map[string]any{"token_type": "Bearer", "access_token": access, "refresh_token": refresh, "expires_in": int(a.cfg.AccessTokenTTL.Seconds())})
}
func (a *application) revoke(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	bearer := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	h := sha256.Sum256([]byte(bearer))
	_, e := a.db.Pool.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE access_hash=$1 AND user_id=$2 AND revoked_at IS NULL`, h[:], u.ID)
	if e != nil {
		jsonError(w, 500, "session_revoke_failed", e.Error(), nil)
		return
	}
	w.WriteHeader(204)
}
func (a *application) me(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	jsonResponse(w, 200, map[string]any{"id": u.ID, "bandbbs_uid": u.BandBBSUID, "username": u.Username, "avatar_url": u.AvatarURL, "role": u.Role, "banned": u.Banned, "created_at": u.CreatedAt})
}
func (a *application) grants(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	providers := []string{}
	bandPublish := false
	githubLogin := ""
	if a.db != nil {
		rows, err := a.db.Pool.Query(r.Context(), `SELECT provider,scopes FROM oauth_grants WHERE user_id=$1`, u.ID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var provider string
				var scopes []string
				if rows.Scan(&provider, &scopes) == nil {
					providers = append(providers, provider)
					for _, scope := range scopes {
						if scope == "resource:write" || scope == "bandbbs_publish" {
							bandPublish = true
						}
					}
				}
			}
		}
		_ = a.db.Pool.QueryRow(r.Context(), `SELECT login FROM github_grants WHERE user_id=$1`, u.ID).Scan(&githubLogin)
		if githubLogin != "" {
			providers = append(providers, "github")
		}
	}
	jsonResponse(w, 200, map[string]any{"role": u.Role, "providers": providers, "bandbbs_publish": bandPublish, "github_login": githubLogin})
}
func (a *application) interactions(w http.ResponseWriter, r *http.Request) {
	id := resourceID(r)
	if a.db == nil {
		jsonError(w, 503, "database_unavailable", "database is unavailable", nil)
		return
	}
	var title, version, typ string
	var downloads, coins, ratingCount int64
	var rating float64
	if e := a.db.Pool.QueryRow(r.Context(), `SELECT title,latest_version,restype,download_count,coin_count,rating,rating_count FROM resource_interactions WHERE resource_id=$1`, id).Scan(&title, &version, &typ, &downloads, &coins, &rating, &ratingCount); e != nil {
		jsonError(w, 404, "resource_not_found", "resource not found", nil)
		return
	}
	hasVoted := false
	if u, err := a.requireUser(r); err == nil {
		_ = a.db.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM resource_coin_votes WHERE resource_id=$1 AND user_id=$2)`, id, u.ID).Scan(&hasVoted)
	}
	jsonResponse(w, 200, map[string]any{"resource_id": id, "title": title, "latest_version": version, "restype": typ, "download_count": downloads, "coin_count": coins, "rating": rating, "rating_count": ratingCount, "has_voted_coin": hasVoted})
}
func (a *application) comments(w http.ResponseWriter, r *http.Request) {
	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	perPage := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && v > 0 && v <= 200 {
		perPage = v
	}
	if a.db == nil {
		jsonResponse(w, 200, map[string]any{"items": []any{}, "pagination": map[string]any{"page": page, "per_page": perPage, "total": 0}})
		return
	}
	rows, e := a.db.Pool.Query(r.Context(), `SELECT c.id,c.user_id,COALESCE(u.username,''),COALESCE(u.avatar_url,''),COALESCE(u.bandbbs_uid,0),c.parent_id,c.content,c.state,c.is_deleted,c.created_at,COALESCE(c.ai_action,'pass'),COALESCE(c.ai_reason,'') FROM resource_comments c LEFT JOIN users u ON u.id=c.user_id WHERE c.resource_id=$1 ORDER BY c.created_at ASC LIMIT 2000`, resourceID(r))
	if e != nil {
		jsonError(w, 500, "comments_failed", e.Error(), nil)
		return
	}
	defer rows.Close()
	type commentNode struct {
		ID         string         `json:"id"`
		UserID     string         `json:"user_id"`
		Username   string         `json:"username"`
		AvatarURL  string         `json:"avatar_url"`
		BandBBSUID int64          `json:"bandbbs_user_id"`
		Content    string         `json:"content"`
		State      string         `json:"state"`
		IsDeleted  bool           `json:"is_deleted"`
		AIAction   string         `json:"ai_action"`
		AIReason   string         `json:"ai_reason"`
		CreatedAt  time.Time      `json:"created_at"`
		Replies    []*commentNode `json:"replies"`
		parent     *string
	}
	nodes := map[string]*commentNode{}
	order := []string{}
	for rows.Next() {
		var id, uid, username, avatar, content, state, aiAction, aiReason string
		var bandBBS int64
		var parent *string
		var deleted bool
		var created time.Time
		if e := rows.Scan(&id, &uid, &username, &avatar, &bandBBS, &parent, &content, &state, &deleted, &created, &aiAction, &aiReason); e != nil {
			continue
		}
		if deleted || state == "hidden" {
			content = ""
		}
		nodes[id] = &commentNode{ID: id, UserID: uid, Username: username, AvatarURL: avatar, BandBBSUID: bandBBS, Content: content, State: state, IsDeleted: deleted, AIAction: aiAction, AIReason: aiReason, CreatedAt: created, Replies: []*commentNode{}, parent: parent}
		order = append(order, id)
	}
	roots := []*commentNode{}
	for _, id := range order {
		n := nodes[id]
		if n.parent != nil {
			if p, ok := nodes[*n.parent]; ok {
				p.Replies = append(p.Replies, n)
				continue
			}
		}
		roots = append(roots, n)
	}
	total := len(roots)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	items := roots[start:end]
	jsonResponse(w, 200, map[string]any{"items": items, "comments": items, "pagination": map[string]any{"page": page, "per_page": perPage, "total": total}})
}
func (a *application) commentCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content  string `json:"content"`
		ParentID string `json:"parent_id"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Content) == "" {
		jsonError(w, 400, "invalid_content", "content is required", nil)
		return
	}
	u, _ := userOf(r)
	id := newID()
	var parent any
	if in.ParentID != "" {
		var parentResource string
		if err := a.db.Pool.QueryRow(r.Context(), `SELECT resource_id FROM resource_comments WHERE id=$1 AND is_deleted=false`, in.ParentID).Scan(&parentResource); err != nil || parentResource != resourceID(r) {
			jsonError(w, 400, "invalid_parent", "parent comment does not belong to this resource", nil)
			return
		}
		parent = in.ParentID
	}

	// 文本 AI 审核与规则校验
	modRes := a.moderateText(r.Context(), in.Content)
	state := "visible"
	isDeleted := false
	modReason := ""
	if modRes.Action == "block" {
		state = "hidden"
		isDeleted = true
		modReason = modRes.Reason
	} else if modRes.Action == "flag" {
		state = "flagged"
		modReason = modRes.Reason
	}

	if _, e := a.db.Pool.Exec(r.Context(), `INSERT INTO resource_comments(id,resource_id,user_id,parent_id,content,state,is_deleted,moderation_reason,ai_action,ai_reason,ai_model) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, resourceID(r), u.ID, parent, in.Content, state, isDeleted, modReason, modRes.Action, modRes.Reason, modRes.Model); e != nil {
		jsonError(w, 500, "comment_create_failed", e.Error(), nil)
		return
	}

	now := time.Now().UTC()
	jsonResponse(w, 201, map[string]any{
		"id":                id,
		"resource_id":       resourceID(r),
		"user": map[string]any{
			"id":          u.ID,
			"username":    u.Username,
			"avatar_url":  u.AvatarURL,
			"bandbbs_uid": u.BandBBSUID,
		},
		"user_id":           u.ID,
		"username":          u.Username,
		"avatar_url":        u.AvatarURL,
		"bandbbs_user_id":   u.BandBBSUID,
		"bandbbs_uid":       u.BandBBSUID,
		"content":           in.Content,
		"body":              in.Content,
		"state":             state,
		"is_deleted":        isDeleted,
		"moderation_reason": modReason,
		"ai_action":         modRes.Action,
		"ai_reason":         modRes.Reason,
		"created_at":        now.Format(time.RFC3339),
		"replies":           []any{},
	})
}
func (a *application) commentDelete(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	id := parts[len(parts)-1]
	var role string
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT role FROM users WHERE id=$1`, u.ID).Scan(&role)
	var e error
	if role == "admin" || role == "reviewer" {
		result, err := a.db.Pool.Exec(r.Context(), `UPDATE resource_comments SET is_deleted=true,state='hidden',moderation_reason='deleted by moderator' WHERE id=$1`, id)
		e = err
		if err == nil && result.RowsAffected() == 0 {
			e = pgx.ErrNoRows
		}
	} else {
		result, err := a.db.Pool.Exec(r.Context(), `UPDATE resource_comments SET is_deleted=true WHERE id=$1 AND user_id=$2`, id, u.ID)
		e = err
		if err == nil && result.RowsAffected() == 0 {
			e = pgx.ErrNoRows
		}
	}
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			jsonError(w, 404, "comment_not_found", "comment not found", nil)
			return
		}
		jsonError(w, 500, "comment_delete_failed", e.Error(), nil)
		return
	}
	w.WriteHeader(204)
}
func (a *application) coins(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	var balance int64
	var lastCheckin *time.Time
	if e := a.db.Pool.QueryRow(r.Context(), `SELECT balance,last_checkin_date FROM user_coin_accounts WHERE user_id=$1`, u.ID).Scan(&balance, &lastCheckin); e != nil {
		balance = 0
	}
	var checked bool
	var streak int
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM daily_coin_checkins WHERE user_id=$1 AND checkin_date=CURRENT_DATE)`, u.ID).Scan(&checked)
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(streak_days,0) FROM daily_coin_checkins WHERE user_id=$1 ORDER BY checkin_date DESC LIMIT 1`, u.ID).Scan(&streak)
	var lastDate any
	if lastCheckin != nil {
		lastDate = lastCheckin.Format("2006-01-02")
	}
	jsonResponse(w, 200, map[string]any{"balance": balance, "last_checkin_date": lastDate, "can_checkin_today": !checked, "streak_days": streak, "checked_in_today": checked, "streak": streak})
}
func (a *application) checkin(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	reward := a.settingInt("daily_checkin_coins", 2)
	var streak int
	alreadyChecked := false
	e := a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, e := tx.Exec(r.Context(), `INSERT INTO user_coin_accounts(user_id,balance) VALUES($1,0) ON CONFLICT DO NOTHING`, u.ID); e != nil {
			return e
		}
		var n int
		e := tx.QueryRow(r.Context(), `SELECT 1 FROM daily_coin_checkins WHERE user_id=$1 AND checkin_date=CURRENT_DATE FOR UPDATE`, u.ID).Scan(&n)
		if e == nil {
			alreadyChecked = true
			return fmt.Errorf("already checked in")
		}
		if e != pgx.ErrNoRows {
			return e
		}
		streak = 1
		_ = tx.QueryRow(r.Context(), `SELECT streak_days+1 FROM daily_coin_checkins WHERE user_id=$1 AND checkin_date=CURRENT_DATE-1`, u.ID).Scan(&streak)
		if _, e = tx.Exec(r.Context(), `INSERT INTO daily_coin_checkins(user_id,checkin_date,coins_awarded,streak_days) VALUES($1,CURRENT_DATE,$2,$3)`, u.ID, reward, streak); e != nil {
			return e
		}
		if _, e = tx.Exec(r.Context(), `UPDATE user_coin_accounts SET balance=balance+$2,last_checkin_date=CURRENT_DATE,updated_at=now() WHERE user_id=$1`, u.ID, reward); e != nil {
			return e
		}
		var updatedBalance int64
		if e = tx.QueryRow(r.Context(), `SELECT balance FROM user_coin_accounts WHERE user_id=$1 FOR UPDATE`, u.ID).Scan(&updatedBalance); e != nil {
			return e
		}
		_, e = tx.Exec(r.Context(), `INSERT INTO coin_ledger(user_id,delta_units,kind,reference_type,reference_id,balance_after,note) VALUES($1,$2,'checkin','checkin',CURRENT_DATE::text,$3,'daily check-in')`, u.ID, reward, updatedBalance)
		return e
	})
	if e != nil {
		if alreadyChecked {
			jsonError(w, 409, "already_checked_in", e.Error(), nil)
		} else {
			jsonError(w, 500, "checkin_failed", e.Error(), nil)
		}
		return
	}
	var balance int64
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT balance FROM user_coin_accounts WHERE user_id=$1`, u.ID).Scan(&balance)
	jsonResponse(w, 200, map[string]any{"coins_awarded": reward, "balance": balance, "streak_days": streak})
}
func (a *application) messages(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	if a.db == nil {
		jsonResponse(w, 200, map[string]any{"messages": []any{}, "unread_count": 0})
		return
	}
	page := 1
	if v, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && v > 0 {
		page = v
	}
	perPage := 20
	if v, err := strconv.Atoi(r.URL.Query().Get("per_page")); err == nil && v > 0 && v <= 100 {
		perPage = v
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	rows, e := a.db.Pool.Query(r.Context(), `SELECT id,kind,event,data,title,body,ref,read_at,created_at FROM user_messages WHERE user_id=$1 AND ($4='' OR kind=$4) ORDER BY created_at DESC LIMIT $2 OFFSET $3`, u.ID, perPage, (page-1)*perPage, kind)
	if e != nil {
		jsonError(w, 500, "messages_failed", e.Error(), nil)
		return
	}
	defer rows.Close()
	messages := []any{}
	for rows.Next() {
		var id, msgKind, event, ref string
		var dataRaw []byte
		var title, body *string
		var readAt *time.Time
		var created time.Time
		if e := rows.Scan(&id, &msgKind, &event, &dataRaw, &title, &body, &ref, &readAt, &created); e != nil {
			continue
		}
		var data any
		if json.Unmarshal(dataRaw, &data) != nil {
			data = map[string]any{}
		}
		messages = append(messages, map[string]any{"id": id, "kind": msgKind, "event": event, "data": data, "title": title, "body": body, "ref": ref, "read_at": readAt, "created_at": created})
	}
	var unread int
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT count(*) FROM user_messages WHERE user_id=$1 AND read_at IS NULL`, u.ID).Scan(&unread)
	jsonResponse(w, 200, map[string]any{"messages": messages, "unread_count": unread})
}
func (a *application) messagesClear(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM user_messages WHERE user_id=$1 AND read_at IS NOT NULL`, u.ID)
	w.WriteHeader(204)
}
func (a *application) messageItem(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/read") || r.Method != "POST" {
		jsonError(w, 404, "not_found", "message not found", nil)
		return
	}
	u, _ := userOf(r)
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	id := parts[len(parts)-2]
	if _, e := a.db.Pool.Exec(r.Context(), `UPDATE user_messages SET read_at=COALESCE(read_at,now()),updated_at=now() WHERE id=$1 AND user_id=$2`, id, u.ID); e != nil {
		jsonError(w, 500, "message_update_failed", e.Error(), nil)
		return
	}
	w.WriteHeader(204)
}
func (a *application) feedback(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	if r.Method == http.MethodGet {
		rows, err := a.db.Pool.Query(r.Context(), `SELECT id,COALESCE(target_source,''),title,content,status,created_at,updated_at FROM feedback_tickets WHERE user_id=$1 ORDER BY updated_at DESC`, u.ID)
		if err != nil {
			jsonError(w, 500, "feedback_list_failed", err.Error(), nil)
			return
		}
		defer rows.Close()
		items := []any{}
		for rows.Next() {
			var id, targetSource, title, content, status string
			var created, updated time.Time
			if rows.Scan(&id, &targetSource, &title, &content, &status, &created, &updated) == nil {
				items = append(items, map[string]any{"id": id, "target_source": targetSource, "title": title, "content": content, "status": status, "created_at": created, "updated_at": updated})
			}
		}
		jsonResponse(w, 200, map[string]any{"items": items})
		return
	}
	var in struct {
		TargetSource string `json:"target_source"`
		TargetID     string `json:"target_id"`
		Title        string `json:"title"`
		Content      string `json:"content"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	title := strings.TrimSpace(in.Title)
	content := strings.TrimSpace(in.Content)
	if title == "" || content == "" {
		jsonError(w, 400, "feedback_fields_required", "title and content are required", nil)
		return
	}
	var id string
	err := a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `INSERT INTO feedback_tickets(user_id,target_source,target_id,title,content) VALUES($1,$2,$3,$4,$5) RETURNING id`, u.ID, in.TargetSource, in.TargetID, title, content).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `INSERT INTO feedback_replies(ticket_id,author_id,message,is_admin) VALUES($1,$2,$3,false)`, id, u.ID, content)
		return err
	})
	if err != nil {
		jsonError(w, 500, "feedback_create_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 201, map[string]any{"id": id, "target_source": in.TargetSource, "target_id": in.TargetID, "title": title, "status": "open"})
}
func (a *application) tip(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Amount int `json:"amount"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Amount <= 0 || in.Amount > 10000 {
		jsonError(w, 400, "invalid_amount", "amount must be between 1 and 10000", nil)
		return
	}
	u, _ := userOf(r)
	resource := resourceID(r)
	var owner string
	var reward int
	var senderAfter, ownerAfter int64
	err := a.db.WithTx(r.Context(), func(tx pgx.Tx) error {
		if e := tx.QueryRow(r.Context(), `SELECT owner_id FROM resource_interactions WHERE resource_id=$1 FOR UPDATE`, resource).Scan(&owner); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), `INSERT INTO user_coin_accounts(user_id,balance) VALUES($1,0) ON CONFLICT DO NOTHING`, u.ID); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), `INSERT INTO user_coin_accounts(user_id,balance) VALUES($1,0) ON CONFLICT DO NOTHING`, owner); e != nil {
			return e
		}
		var balance int64
		if e := tx.QueryRow(r.Context(), `SELECT balance FROM user_coin_accounts WHERE user_id=$1 FOR UPDATE`, u.ID).Scan(&balance); e != nil {
			return e
		}
		if balance < int64(in.Amount) {
			return fmt.Errorf("insufficient balance")
		}
		senderAfter = balance - int64(in.Amount)
		reward = in.Amount / 10
		if owner == u.ID {
			ownerAfter = balance + int64(reward) - int64(in.Amount)
		} else if e := tx.QueryRow(r.Context(), `SELECT balance FROM user_coin_accounts WHERE user_id=$1 FOR UPDATE`, owner).Scan(&ownerAfter); e != nil {
			return e
		}
		if owner != u.ID {
			ownerAfter += int64(reward)
		}
		if _, e := tx.Exec(r.Context(), `UPDATE user_coin_accounts SET balance=balance-$1,updated_at=now() WHERE user_id=$2`, in.Amount, u.ID); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), `UPDATE user_coin_accounts SET balance=balance+$1,updated_at=now() WHERE user_id=$2`, reward, owner); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), `INSERT INTO resource_coin_votes(resource_id,user_id,amount) VALUES($1,$2,$3)`, resource, u.ID, in.Amount); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), `UPDATE resource_interactions SET coin_count=coin_count+$1,updated_at=now() WHERE resource_id=$2`, in.Amount, resource); e != nil {
			return e
		}
		if _, e := tx.Exec(r.Context(), `INSERT INTO coin_ledger(user_id,delta_units,kind,reference_type,reference_id,note,balance_after) VALUES($1,$2,'tip_send','resource',$3,'resource tip',$4),($5,$6,'tip_receive','resource',$3,'creator reward',$7)`, u.ID, -in.Amount, resource, senderAfter, owner, reward, ownerAfter); e != nil {
			return e
		}
		_, e := tx.Exec(r.Context(), `INSERT INTO user_messages(user_id,kind,event,data,ref) VALUES($1,'coin','coin.tip_received',jsonb_build_object('resource_id',$2::text,'amount',$3::int,'creator_reward',$4::int,'sender',jsonb_build_object('user_id',$5::text,'username',$6::text,'avatar_url',$7::text)),$2)`, owner, resource, in.Amount, reward, u.ID, u.Username, u.AvatarURL)
		return e
	})
	if err != nil {
		if strings.Contains(err.Error(), "insufficient") {
			jsonError(w, 400, "INSUFFICIENT_BALANCE", "insufficient coin balance", nil)
		} else {
			jsonError(w, 500, "coin_transfer_failed", err.Error(), nil)
		}
		return
	}
	var resourceCoins, userBalance int64
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT coin_count FROM resource_interactions WHERE resource_id=$1`, resource).Scan(&resourceCoins)
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT balance FROM user_coin_accounts WHERE user_id=$1`, u.ID).Scan(&userBalance)
	jsonResponse(w, 200, map[string]any{"resource_coin_count": resourceCoins, "user_balance": userBalance, "creator_reward": reward})
}
func (a *application) coinStatus(w http.ResponseWriter, r *http.Request) {
	u, _ := userOf(r)
	var amount int64
	var voted bool
	if err := a.db.Pool.QueryRow(r.Context(), `SELECT COALESCE(SUM(amount),0),EXISTS(SELECT 1 FROM resource_coin_votes WHERE resource_id=$1 AND user_id=$2) FROM resource_coin_votes WHERE resource_id=$1`, resourceID(r), u.ID).Scan(&amount, &voted); err != nil {
		jsonError(w, 500, "coin_status_failed", err.Error(), nil)
		return
	}
	jsonResponse(w, 200, map[string]any{"amount": amount, "voted": voted})
}
func (a *application) rating(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Rating int `json:"rating"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Rating < 1 || in.Rating > 5 {
		jsonError(w, 400, "invalid_rating", "rating must be between 1 and 5", nil)
		return
	}
	var newRating float64
	var ratingCount int64
	e := a.db.Pool.QueryRow(r.Context(), `UPDATE resource_interactions SET rating=(rating*rating_count+$1)/(rating_count+1),rating_count=rating_count+1,updated_at=now() WHERE resource_id=$2 RETURNING rating,rating_count`, in.Rating, resourceID(r)).Scan(&newRating, &ratingCount)
	if e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			jsonError(w, 404, "resource_not_found", "resource not found", nil)
			return
		}
		jsonError(w, 500, "rating_failed", e.Error(), nil)
		return
	}
	jsonResponse(w, 200, map[string]any{"new_rating": newRating, "rating_count": ratingCount})
}
func (a *application) download(w http.ResponseWriter, r *http.Request) {
	u, logged := userOf(r)
	var uid any
	if logged {
		uid = u.ID
	}
	ip := clientIP(r)
	resource := resourceID(r)
	var seen bool
	_ = a.db.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM download_events WHERE resource_id=$1 AND ip=$2 AND created_at >= date_trunc('day', now()))`, resource, ip).Scan(&seen)
	if _, e := a.db.Pool.Exec(r.Context(), `INSERT INTO download_events(resource_id,user_id,ip,user_agent) VALUES($1,$2,$3,$4)`, resource, uid, ip, r.UserAgent()); e != nil {
		jsonError(w, 500, "download_event_failed", e.Error(), nil)
		return
	}
	if !seen {
		_, _ = a.db.Pool.Exec(r.Context(), `UPDATE resource_interactions SET download_count=download_count+1,updated_at=now() WHERE resource_id=$1`, resource)
	}
	jsonResponse(w, 200, map[string]string{"status": "recorded"})
}
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if e := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(v); e != nil {
		jsonError(w, 400, "invalid_json", "request body must be valid JSON", nil)
		return false
	}
	return true
}
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
func token(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return newID()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func hash(v string) []byte { h := sha256.Sum256([]byte(v)); return h[:] }
func resourceID(r *http.Request) string {
	p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, v := range p {
		if v == "resources" && i+1 < len(p) {
			return p[i+1]
		}
	}
	return ""
}
func escape(v string) string {
	return strings.NewReplacer("%", "%25", " ", "%20", "?", "%3F", "&", "%26").Replace(v)
}
