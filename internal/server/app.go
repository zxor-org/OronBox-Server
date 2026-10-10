package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/attestation"
	apiAuth "github.com/zxor-org/OronBox-Server/internal/api/auth"
	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type application struct {
	cfg         config.Config
	db          *store.Store
	started     time.Time
	cipher      *auth.Cipher
	states      sync.Map
	limits      *rateLimiter
	nonces      *attestation.NonceStore
	revMu       sync.RWMutex
	revoked     attestation.Revocation
	minimum     string
	adminAuthFn func(*http.Request) bool
}

func newApplication(d Dependencies) *application {
	c, _ := auth.NewCipher(d.Config.TokenEncryptionKey)
	return &application{
		cfg:     d.Config,
		db:      d.Store,
		started: d.StartedAt,
		cipher:  c,
		limits:  newRateLimiter(),
		nonces:  attestation.NewNonceStore(d.Config.ClientAttestationSkew),
		revoked: attestation.NewRevocation(nil),
	}
}

func (a *application) withAttestation(next http.Handler) http.Handler {
	if !a.cfg.ClientAttestationEnabled {
		return next
	}
	a.revMu.RLock()
	rev, min := a.revoked, a.minimum
	a.revMu.RUnlock()
	return attestation.Middleware{
		Options: attestation.Options{
			MasterKey:      a.cfg.AttestationMasterKey,
			NonceStore:     a.nonces,
			Skew:           a.cfg.ClientAttestationSkew,
			Revocation:     rev,
			MinimumVersion: min,
		},
		Next: next,
	}
}

func (a *application) refreshRevocation() {
	if a.db == nil || a.db.Pool == nil {
		return
	}
	// Spec client-attestation-spec.md §6.2: auth_revoked_keys
	var raw json.RawMessage
	if err := a.db.Pool.QueryRow(context.Background(), `
		SELECT value FROM server_settings WHERE key = 'auth_revoked_keys'
	`).Scan(&raw); err == nil && len(raw) > 0 {
		var list []string
		if err := json.Unmarshal(raw, &list); err == nil {
			a.revMu.Lock()
			a.revoked = attestation.NewRevocation(list)
			a.revMu.Unlock()
		}
	}

	// Spec client-attestation-spec.md §6.1: auth_minimum_version
	var minVer string
	if err := a.db.Pool.QueryRow(context.Background(), `
		SELECT value->>'minimum_version' FROM server_settings WHERE key = 'auth_minimum_version'
	`).Scan(&minVer); err == nil {
		a.revMu.Lock()
		a.minimum = minVer
		a.revMu.Unlock()
	}
}

func (a *application) runCoordinator(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.db != nil && a.db.Pool != nil {
				_, _ = a.db.Pool.Exec(ctx, `
					DELETE FROM blobs WHERE ref_count <= 0 AND created_at < now() - interval '24 hours'
				`)
			}
		}
	}
}

func (a *application) finishAdminLogin(w http.ResponseWriter, r *http.Request, code string) {
	authSvc := apiAuth.NewService(a.cfg, a.db, a.cipher, &a.states)
	uid, username, avatar, accessToken, err := authSvc.FetchBandBBSUserWithToken(r.Context(), code, a.cfg.BandBBS.RedirectURI)
	if err != nil {
		a.renderConsoleError(w, r, err.Error(), "返回登录", "/console")
		return
	}

	if accessToken != "" && a.cfg.BandBBS.RevokeURL != "" {
		go func(tok string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = authSvc.RevokeBandBBSToken(ctx, tok)
		}(accessToken)
	}

	allowed := false
	for _, v := range a.cfg.AdminBandBBSUIDs {
		if v == uid {
			allowed = true
			break
		}
	}
	if !allowed {
		a.renderConsoleError(w, r, "该账号未被授权管理员权限", "返回登录", "/console")
		return
	}

	intUID, _ := strconv.Atoi(uid)
	_, _ = a.db.Pool.Exec(r.Context(), `
		INSERT INTO users (bandbbs_uid, username, avatar_url, role, updated_at)
		VALUES ($1, $2, $3, 'admin', now())
		ON CONFLICT (bandbbs_uid) DO UPDATE 
		SET username = EXCLUDED.username, avatar_url = EXCLUDED.avatar_url, role = 'admin', updated_at = now()
	`, intUID, username, avatar)

	sessBytes := make([]byte, 32)
	_, _ = rand.Read(sessBytes)
	sessToken := hex.EncodeToString(sessBytes)
	sessHash := sha256.Sum256([]byte(sessToken))

	_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM admin_sessions WHERE user_uid = $1`, intUID)
	_, err = a.db.Pool.Exec(r.Context(), `
		INSERT INTO admin_sessions (session_hash, user_uid, username, ip, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '24 hours')
	`, sessHash[:], intUID, username, r.RemoteAddr)

	if err != nil {
		a.renderConsoleError(w, r, "创建管理会话失败", "重试", "/console")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oronbox_admin",
		Value:    sessToken,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/console/", http.StatusFound)
}

func (a *application) isAdminAuthenticated(r *http.Request) bool {
	if a.adminAuthFn != nil {
		return a.adminAuthFn(r)
	}
	cookie, err := r.Cookie("oronbox_admin")
	if err != nil || cookie.Value == "" || a.db == nil || a.db.Pool == nil {
		return false
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	var uid int
	err = a.db.Pool.QueryRow(r.Context(), `
		SELECT user_uid FROM admin_sessions WHERE session_hash = $1 AND expires_at > now()
	`, hash[:]).Scan(&uid)
	return err == nil && uid > 0
}

func (a *application) adminAuthorize(w http.ResponseWriter, r *http.Request) {
	stateBytes := make([]byte, 16)
	_, _ = rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)
	a.states.Store("admin_"+state, time.Now().Add(10*time.Minute))

	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=admin_%s",
		a.cfg.BandBBS.AuthorizeURL,
		url.QueryEscape(a.cfg.BandBBS.ClientID),
		url.QueryEscape(a.cfg.BandBBS.RedirectURI),
		url.QueryEscape(a.cfg.BandBBS.Scopes),
		url.QueryEscape(state),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *application) adminLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("oronbox_admin")
	if err == nil && cookie.Value != "" {
		hash := sha256.Sum256([]byte(cookie.Value))
		_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM admin_sessions WHERE session_hash = $1`, hash[:])
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oronbox_admin",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (a *application) adminSession(w http.ResponseWriter, r *http.Request) {
	uid, username, err := httputil.AuthenticateAdmin(r.Context(), a.db, r)
	if err != nil || uid <= 0 {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"uid":           uid,
		"username":      username,
	})
}

func (a *application) adminRefresh(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (a *application) releaseSync(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "synced"})
}
