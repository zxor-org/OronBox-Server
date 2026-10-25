package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/model"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

const consoleOAuthProvider = "bandbbs_console"

func consoleStateCookieName(state string) string {
	sum := sha256.Sum256([]byte(state))
	return "oronbox_console_oauth_" + hex.EncodeToString(sum[:12])
}
func (a *App) consoleStateCookie(r *http.Request, state, value string, expires time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{Name: consoleStateCookieName(state), Value: value, Path: "/oauth2/bandbbs/callback", HttpOnly: true, Secure: a.secureAdminCookie(r), SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: maxAge}
}
func (a *App) handleAdminAuthorize(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		http.Error(w, "authorization temporarily unavailable", 503)
		return
	}
	state = "admin_" + state
	binding, err := randomToken()
	if err != nil {
		http.Error(w, "authorization temporarily unavailable", 503)
		return
	}
	expiry := time.Now().UTC().Add(a.cfg.StateTTL)
	if a.cfg.StateTTL <= 0 {
		expiry = time.Now().UTC().Add(10 * time.Minute)
	}
	err = a.store.CreateState(r.Context(), store.CreateStateParams{ID: state, Provider: consoleOAuthProvider, Purpose: "console_login", ExpiresAt: expiry, Meta: model.ClientMeta{AppID: "oronbox-console", Platform: "web", IP: a.clientIP(r), UA: r.UserAgent()}, ReturnURI: "/console/", SecretCipher: authcore.HashToken(binding, a.cfg.SessionSecret)})
	if err != nil {
		http.Error(w, "authorization temporarily unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.SetCookie(w, a.consoleStateCookie(r, state, binding, expiry, 0))
	http.Redirect(w, r, a.bandbbs.AuthorizeURL(state, []string{"user:read"}), http.StatusFound)
}
func (a *App) consoleLoginFailure(w http.ResponseWriter, r *http.Request, code string) {
	oauthLog(r.Context()).Warn("console oauth rejected", "reason", code)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, "/console/login?error="+code, http.StatusSeeOther)
}
func (a *App) handleConsoleBandBBSCallback(w http.ResponseWriter, r *http.Request) {
	stateID := r.URL.Query().Get("state")
	state, err := a.store.ConsumeState(r.Context(), consoleOAuthProvider, stateID)
	binding, cookieErr := r.Cookie(consoleStateCookieName(stateID))
	http.SetCookie(w, a.consoleStateCookie(r, stateID, "", time.Time{}, -1))
	if err != nil || state.Purpose != "console_login" || cookieErr != nil || !constantTimeEqual(string(state.SecretCipher), string(authcore.HashToken(binding.Value, a.cfg.SessionSecret))) {
		a.consoleLoginFailure(w, r, "invalid_state")
		return
	}
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		a.consoleLoginFailure(w, r, "authorization_failed")
		return
	}
	token, err := a.bandbbs.ExchangeCode(r.Context(), code)
	if err != nil {
		a.consoleLoginFailure(w, r, "authorization_failed")
		return
	}
	defer a.revokePair(r, token)
	subject, _, err := a.bandbbs.ValidateScopes(r.Context(), token, []string{"user:read"})
	if err != nil {
		a.consoleLoginFailure(w, r, "scope_mismatch")
		return
	}
	profile, err := a.bandbbs.Me(r.Context(), token.AccessToken)
	if err != nil {
		a.consoleLoginFailure(w, r, "identity_failed")
		return
	}
	if subject != "" && subject != fmt.Sprint(profile.UserID) {
		a.consoleLoginFailure(w, r, "identity_failed")
		return
	}
	user, err := a.store.UpsertUser(r.Context(), store.UpsertUserParams{BandBBSUserID: profile.UserID, Username: profile.Username, AvatarURL: preferredAvatar(profile.AvatarURLs)})
	if err != nil {
		a.consoleLoginFailure(w, r, "authorization_failed")
		return
	}
	bootstrap := containsInt64(a.cfg.Admin.BandBBSUserIDs, user.BandBBSUserID)
	if user.BannedAt != nil || (!bootstrap && user.Role != "admin" && user.Role != "reviewer") {
		a.consoleLoginFailure(w, r, "not_authorized")
		return
	}
	if bootstrap && user.Role != "admin" {
		user, err = a.store.SetUserRole(r.Context(), user.ID, "admin")
		if err != nil {
			a.consoleLoginFailure(w, r, "authorization_failed")
			return
		}
	}
	credential, err := randomToken()
	if err != nil {
		a.consoleLoginFailure(w, r, "authorization_failed")
		return
	}
	expires := time.Now().UTC().Add(store.AdminIdleTTL)
	if err = a.store.CreateAdminSession(r.Context(), credential, user.ID, user.Username, a.clientIP(r), r.UserAgent(), expires); err != nil {
		a.consoleLoginFailure(w, r, "authorization_failed")
		return
	}
	actor := store.AdminSession{ID: store.AdminCredentialHash(credential), UserID: user.ID, Username: user.Username, ExpiresAt: expires}
	_ = a.store.RecordAudit(r.Context(), actor, "admin_login", "success", a.clientIP(r), r.UserAgent(), "")
	http.SetCookie(w, a.adminSessionCookie(r, credential, expires, 0))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, "/console/", http.StatusSeeOther)
}
func (a *App) rotatedAdminCredential(id string, generation int64) string {
	return base64.RawURLEncoding.EncodeToString(authcore.HashToken("admin-rotation\x00"+id+"\x00"+strconv.FormatInt(generation, 10), a.cfg.SessionSecret))
}
func (a *App) handleAdminRefresh(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	cookie, err := r.Cookie(adminCookieName)
	if err != nil {
		a.rejectAdminAuth(w, r)
		return
	}
	next := a.rotatedAdminCredential(actor.ID, actor.Generation+1)
	session, err := a.store.RotateAdminSession(r.Context(), actor.ID, store.AdminCredentialHash(cookie.Value), store.AdminCredentialHash(next), actor.Generation)
	if errors.Is(err, sql.ErrNoRows) {
		a.rejectAdminAuth(w, r)
		return
	}
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("session_unavailable", "session refresh temporarily unavailable"))
		return
	}
	credential := a.rotatedAdminCredential(session.ID, session.Generation)
	http.SetCookie(w, a.adminSessionCookie(r, credential, session.ExpiresAt, 0))
	r = r.WithContext(context.WithValue(r.Context(), adminContextKey{}, session))
	a.handleAdminAPISession(w, r)
}
