package server

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAuthSuccess(w http.ResponseWriter, r *http.Request) {
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "授权完成",
		Heading:     "授权完成",
		Description: "可以返回 OronBox 继续使用",
		Tone:        "success",
	})
}

func (a *App) handleAuthFailed(w http.ResponseWriter, r *http.Request) {
	a.renderTransition(w, r, web.TransitionPageData{
		Title:       "授权失败",
		Heading:     "授权失败",
		Description: authErrorMessage(r.URL.Query().Get("error")),
		Tone:        "danger",
	})
}

func (a *App) renderTransition(w http.ResponseWriter, r *http.Request, data web.TransitionPageData) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Content-Security-Policy", transitionCSP)
	a.render(w, r, "transition_page", data)
}

func (a *App) loginTransitionTarget(returnURI string, ticket string) (template.URL, error) {
	if !a.allowedReturnURI(returnURI) {
		return "", fmt.Errorf("untrusted OAuth return URI")
	}
	// returnURI is restricted to configured native, admin, or web callbacks.
	// appendQuery only adds the newly-created one-time login ticket.
	return template.URL(appendQuery(returnURI, "ticket", ticket)), nil
}

func authErrorMessage(code string) string {
	switch strings.TrimSpace(code) {
	case "invalid_callback":
		return "授权回调缺少必要信息，请重新发起登录"
	case "invalid_state":
		return "授权请求已失效或已被使用，请重新发起登录"
	case "token_exchange_failed":
		return "未能完成授权凭证交换，请稍后重试"
	case "scope_mismatch":
		return "授权范围不完整，请重新授权所需权限"
	case "identity_failed", "identity_save_failed":
		return "未能读取或保存账号信息，请稍后重试"
	case "account_mismatch":
		return "授权账号与当前登录账号不一致"
	case "account_banned":
		return "账号已被封禁，如有疑问请通过工单联系管理员"
	case "grant_encrypt_failed", "grant_save_failed", "ticket_create_failed":
		return "服务端未能保存授权结果，请稍后重试"
	default:
		return "未能完成授权，请返回 OronBox 后重试"
	}
}

// render writes a page. The request is required because every rendered POST
// form carries a session-bound CSRF token, which can only be derived from the
// admin session attached to the request context.
func (a *App) render(w http.ResponseWriter, r *http.Request, name string, data any) {
	if values, ok := data.(map[string]any); ok {
		if _, exists := values["PageSizes"]; !exists {
			values["PageSizes"] = []int{25, 50, 100}
		}
		if _, exists := values["CSRFToken"]; !exists {
			values["CSRFToken"] = a.adminCSRFTokenFor(r)
		}
		// The drawer is derived from the session rather than passed in by each
		// handler, so no page can accidentally offer a reviewer a link that
		// only answers with a 403.
		role, _ := r.Context().Value(adminRoleContextKey{}).(string)
		values["Role"] = role
		values["Nav"] = web.NavigationFor(role)
		values["HomePath"] = web.HomePathFor(role)
	}
	if err := a.templates.Render(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleRoot(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "server_home", map[string]any{"Title": "Server"})
}

func (a *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	statusCode := http.StatusOK
	if err := a.store.Ping(r.Context()); err != nil {
		status = "database_error"
		statusCode = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, statusCode, map[string]any{
		"status":     status,
		"version":    a.cfg.Version,
		"commit":     a.cfg.Commit,
		"started_at": a.startedAt.Format(time.RFC3339),
	})
}
