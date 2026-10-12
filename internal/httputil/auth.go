package httputil

import (
	"context"
	"crypto/sha256"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type contextKey string

const (
	userUIDKey  contextKey = "user_uid"
	adminUIDKey contextKey = "admin_uid"
)

// AuthenticateUser verifies bearer token against sessions table.
func AuthenticateUser(ctx context.Context, s *store.Store, token string) (int, error) {
	if s == nil || s.Pool == nil || token == "" {
		return 0, pgx.ErrNoRows
	}
	hash := sha256.Sum256([]byte(token))
	var uid int
	err := s.Pool.QueryRow(ctx, `
		SELECT s.user_uid FROM sessions s
		JOIN users u ON u.bandbbs_uid = s.user_uid
		WHERE s.access_hash = $1 AND s.access_expires_at > now()
		  AND s.revoked_at IS NULL AND u.banned = false
	`, hash[:]).Scan(&uid)
	return uid, err
}

// AuthenticateAdmin verifies cookie token against admin_sessions table.
func AuthenticateAdmin(ctx context.Context, s *store.Store, r *http.Request) (int, string, error) {
	uid, username, _, err := AuthenticateAdminRole(ctx, s, r)
	return uid, username, err
}

// AuthenticateAdminRole verifies the isolated admin session and returns the
// current database role for route-level authorization.
func AuthenticateAdminRole(ctx context.Context, s *store.Store, r *http.Request) (int, string, string, error) {
	if s == nil || s.Pool == nil {
		return 0, "", "", pgx.ErrNoRows
	}
	cookie, err := r.Cookie("oronbox_admin")
	if err != nil || cookie.Value == "" {
		return 0, "", "", pgx.ErrNoRows
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	var uid int
	var username, role string
	err = s.Pool.QueryRow(ctx, `
		SELECT a.user_uid, a.username, u.role FROM admin_sessions a
		JOIN users u ON u.bandbbs_uid = a.user_uid
		WHERE a.session_hash = $1 AND a.expires_at > now() AND u.banned = false
	`, hash[:]).Scan(&uid, &username, &role)
	return uid, username, role, err
}

// WithUserUID returns a new context with userUIDKey set.
func WithUserUID(ctx context.Context, uid int) context.Context {
	return context.WithValue(ctx, userUIDKey, uid)
}

// UserUIDFromContext returns user uid from request context if set.
func UserUIDFromContext(ctx context.Context) (int, bool) {
	val := ctx.Value(userUIDKey)
	if uid, ok := val.(int); ok && uid > 0 {
		return uid, true
	}
	return 0, false
}
