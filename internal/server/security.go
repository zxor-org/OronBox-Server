package server

import (
	"net/http"

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
			header.Set("Content-Security-Policy", baseCSP)
		}
		if https {
			header.Set("Strict-Transport-Security", hstsValue)
		}
		next.ServeHTTP(w, r)
	})
}
