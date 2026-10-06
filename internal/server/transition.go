package server

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/web"
)

const transitionCSP = "default-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'"

var defaultTemplates = web.NewTemplates()

func (a *application) renderTransition(w http.ResponseWriter, r *http.Request, data web.TransitionPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("Content-Security-Policy", transitionCSP)
	if err := defaultTemplates.Render(w, "transition_page", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *application) handleHome(w http.ResponseWriter, r *http.Request) {
	target := "https://oronbox.zxor.org"
	for _, origin := range a.cfg.WebClientOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" && !strings.Contains(origin, "ob-api") {
			target = origin
			break
		}
	}
	http.Redirect(w, r, target, http.StatusFound)
}


func authErrorMessage(code string) string {
	switch strings.TrimSpace(code) {
	case "invalid_callback":
		return "授权回调缺少必要信息，请重新发起登录"
	case "invalid_state":
		return "授权请求已失效或已被使用，请重新发起登录"
	case "token_exchange_failed", "oauth_exchange_failed":
		return "未能完成授权凭证交换，请稍后重试"
	case "scope_mismatch":
		return "授权范围不完整，请重新授权所需权限"
	case "identity_failed", "identity_save_failed", "user_create_failed":
		return "未能读取或保存账号信息，请稍后重试"
	case "account_mismatch":
		return "授权账号与当前登录账号不一致"
	case "account_banned":
		return "账号已被封禁，如有疑问请通过工单联系管理员"
	case "grant_encrypt_failed", "grant_save_failed", "ticket_create_failed":
		return "服务端未能保存授权结果，请稍后重试"
	case "access_denied":
		return "用户取消了授权"
	default:
		if code != "" {
			return fmt.Sprintf("未能完成授权 (%s)，请返回 OronBox 后重试", code)
		}
		return "未能完成授权，请返回 OronBox 后重试"
	}
}
