package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
)

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
		uid, username, err := httputil.AuthenticateAdmin(r.Context(), a.db, r)
		if err != nil || uid <= 0 {
			if strings.HasPrefix(r.Header.Get("Accept"), "text/html") {
				a.renderAdminLogin(w, r)
				return
			}
			httputil.Error(w, http.StatusUnauthorized, "admin_unauthorized", "管理员身份验证失败")
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
			if recover() != nil {
				httputil.Error(w, http.StatusInternalServerError, "internal_error", "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
