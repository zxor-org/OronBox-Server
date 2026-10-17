package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/config"
)

const (
	// baseCSP denies everything the console does not actually use. Scripts and
	// styles are same-origin only: icons are an inline sprite and type uses
	// system fonts, so a font CDN is not part of the policy.
	baseCSP = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'" +
		"; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

	// transitionCSP drops connect-src as well because the OAuth handoff page
	// only redirects and never talks to the API.
	transitionCSP = "default-src 'none'; script-src 'self'; style-src 'self'; base-uri 'none'; frame-ancestors 'none'"

	// hstsValue omits preload: that is a public-list commitment the operator
	// should make deliberately, not something a deploy silently turns on.
	hstsValue = "max-age=31536000; includeSubDomains"
)

// SecurityHeaders applies the response headers that do not depend on the
// handler. Strict transport security is only sent when the public entrypoint is
// HTTPS, since pinning it on a plain-HTTP deployment locks users out.
func SecurityHeaders(cfg config.Config, next http.Handler) http.Handler {
	https := cfg.ServesHTTPS()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("X-Frame-Options", "DENY")
		if header.Get("Content-Security-Policy") == "" {
			header.Set("Content-Security-Policy", contentCSP(cfg))
		}
		if https {
			header.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}

func contentCSP(cfg config.Config) string {
	origin, err := url.Parse(cfg.DeviceCatalog.BaseURL)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil {
		return baseCSP
	}
	return strings.Replace(baseCSP, "img-src 'self' data:", "img-src 'self' data: "+origin.Scheme+"://"+origin.Host, 1)
}

func CORS(origins []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(origins))
	for _, origin := range origins {
		allowed[strings.TrimRight(origin, "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(strings.TrimSpace(r.Header.Get("Origin")), "/")
		if origin != "" && (allowed[origin] || allowed["*"]) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-Match, X-OronBox-App-Id, X-OronBox-Version, X-OronBox-Build, X-OronBox-Platform, X-OB-Tag, X-OB-Commit, X-OB-Timestamp, X-OB-Nonce, X-OB-Signature")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			if origin == "" || (!allowed[origin] && !allowed["*"]) {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		host = strings.TrimSpace(r.RemoteAddr)
	}
	peer := net.ParseIP(host)
	trusted := false
	for _, network := range a.trustedProxies {
		if peer != nil && network.Contains(peer) {
			trusted = true
			break
		}
	}
	if trusted {
		if value := validForwardedIP(r.Header.Get("CF-Connecting-IP")); value != "" {
			return value
		}
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			for _, value := range strings.Split(forwarded, ",") {
				if value = validForwardedIP(value); value != "" {
					return value
				}
			}
		}
		if value := validForwardedIP(r.Header.Get("X-Real-IP")); value != "" {
			return value
		}
	}
	return host
}

func validForwardedIP(value string) string {
	value = strings.TrimSpace(value)
	if net.ParseIP(value) == nil {
		return ""
	}
	return value
}
