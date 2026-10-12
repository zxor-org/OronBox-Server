package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/observability"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
}

func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if len(id) < 8 || len(id) > 64 || strings.ContainsAny(id, "\r\n \t") {
			id = newRequestID()
		}
		ctx := observability.WithRequestID(r.Context(), id)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", id)
		recorder := &responseRecorder{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		attrs := []any{"request_id", id, "method", r.Method, "path", r.URL.Path, "status", status, "bytes", recorder.bytes, "duration_ms", time.Since(started).Milliseconds()}
		if uid, ok := httputil.UserUIDFromContext(r.Context()); ok {
			attrs = append(attrs, "user_uid", uid)
		}
		observabilityLog().Info("http request", attrs...)
	})
}

func observabilityLog() *slog.Logger { return slog.Default() }

func authRequired(app *application, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		if token == "" {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "缺少 Authorization 令牌")
			return
		}
		uid, err := httputil.AuthenticateUser(r.Context(), app.db, token)
		if err != nil || uid <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "无效或过期的授权令牌")
			return
		}

		ctx := httputil.WithUserUID(r.Context(), uid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (a *application) adminProtected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, username, role, err := httputil.AuthenticateAdminRole(r.Context(), a.db, r)
		if err != nil || uid <= 0 {
			if strings.HasPrefix(r.Header.Get("Accept"), "text/html") {
				a.renderAdminLogin(w, r)
				return
			}
			httputil.Error(w, http.StatusUnauthorized, "admin_unauthorized", "管理员身份验证失败")
			return
		}
		reviewerAllowed := strings.HasPrefix(r.URL.Path, "/admin/api/reviews") ||
			(strings.HasPrefix(r.URL.Path, "/admin/api/plugins/") && strings.HasSuffix(r.URL.Path, "/download") && r.Method == http.MethodGet) ||
			(r.URL.Path == "/admin/api/plugins" && r.Method == http.MethodGet) ||
			(strings.HasPrefix(r.URL.Path, "/admin/api/plugins/") && strings.HasSuffix(r.URL.Path, "/review") && r.Method == http.MethodPost)
		if role != "admin" && role != "superadmin" && !(role == "reviewer" && reviewerAllowed) {
			httputil.Error(w, http.StatusForbidden, "forbidden", "管理员权限不足")
			return
		}

		ctx := httputil.WithUserUID(r.Context(), uid)
		ctx = context.WithValue(ctx, "admin_username", username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				observabilityLog().Error("panic recovered", "request_id", observability.RequestID(r.Context()), "method", r.Method, "path", r.URL.Path, "panic", recovered)
				httputil.Error(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
