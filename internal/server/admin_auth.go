package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	apiAuth "github.com/zxor-org/OronBox-Server/internal/api/auth"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/observability"
)

func (a *application) finishAdminLogin(w http.ResponseWriter, r *http.Request, code string) {
	authSvc := apiAuth.NewService(a.cfg, a.db, a.cipher, &a.states)
	uid, username, avatar, accessToken, _, _, err := authSvc.FetchBandBBSUserWithToken(r.Context(), code, a.cfg.BandBBS.RedirectURI)
	if err != nil {
		a.logger.Error("admin bandbbs oauth callback failed", "request_id", observability.RequestID(r.Context()), "error", observability.SafeError(err))
		a.renderConsoleError(w, r, "第三方授权服务暂时不可用", "返回登录", "/console")
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
	var existingRole string
	roleErr := a.db.Pool.QueryRow(r.Context(), `SELECT role FROM users WHERE bandbbs_uid = $1`, intUID).Scan(&existingRole)
	if roleErr == pgx.ErrNoRows {
		_, err = a.db.Pool.Exec(r.Context(), `INSERT INTO users (bandbbs_uid, username, avatar_url, role, updated_at) VALUES ($1, $2, $3, 'superadmin', now())`, intUID, username, avatar)
	} else if roleErr == nil {
		_, err = a.db.Pool.Exec(r.Context(), `UPDATE users SET username = $2, avatar_url = $3, updated_at = now() WHERE bandbbs_uid = $1`, intUID, username, avatar)
	} else {
		err = roleErr
	}
	if err != nil {
		a.renderConsoleError(w, r, "保存管理员信息失败", "重试", "/console")
		return
	}

	sessBytes := make([]byte, 32)
	if _, err = rand.Read(sessBytes); err != nil {
		a.renderConsoleError(w, r, "无法生成安全会话", "重试", "/console")
		return
	}
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

	secureCookie := true
	base := a.cfg.BaseURL
	if base == "" {
		base = a.cfg.PublicURL
	}
	if parsed, parseErr := url.Parse(base); parseErr == nil && parsed.Scheme == "http" {
		secureCookie = false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oronbox_admin",
		Value:    sessToken,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/console/", http.StatusFound)
}

func (a *application) isAdminAuthenticated(r *http.Request) bool {
	if a.adminAuthFn != nil {
		return a.adminAuthFn(r)
	}
	uid, _, role, err := httputil.AuthenticateAdminRole(r.Context(), a.db, r)
	return err == nil && uid > 0 && (role == "admin" || role == "superadmin" || role == "reviewer")
}

func (a *application) adminAuthorize(w http.ResponseWriter, r *http.Request) {
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		httputil.Error(w, http.StatusInternalServerError, "state_generation_failed", "无法生成安全授权状态")
		return
	}
	state := hex.EncodeToString(stateBytes)
	stateTTL := a.cfg.StateTTL
	if stateTTL <= 0 {
		stateTTL = 10 * time.Minute
	}
	a.states.Store("admin_"+state, time.Now().Add(stateTTL))

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
	uid, username, role, err := httputil.AuthenticateAdminRole(r.Context(), a.db, r)
	if err != nil || uid <= 0 || (role != "admin" && role != "superadmin" && role != "reviewer") {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"uid":           uid,
		"bandbbs_uid":   uid,
		"username":      username,
		"role":          role,
	})
}

func (a *application) adminRefresh(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}
