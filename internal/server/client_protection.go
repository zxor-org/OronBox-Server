package server

import (
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/attestation"
)

// Content URLs are also consumed by browser image widgets and external package
// installers. They remain public; handleBlob retains publication and quota checks.
func publicDelivery(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	if strings.HasPrefix(p, "/api/v1/blobs/") && len(strings.Split(p, "/")) == 5 {
		return true
	}
	parts := strings.Split(p, "/")
	return len(parts) == 6 && parts[1] == "api" && parts[2] == "v1" && parts[3] == "plugins" && (parts[5] == "icon" || parts[5] == "package")
}

func (a *App) protectClientAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveLegacyNotice(w, r) {
			return
		}
		if r.URL.Path == "/api" || (strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/v1/")) {
			http.NotFound(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Attach signature checks to actual registered routes so an unknown path stays
// an ordinary ServeMux 404, without a synthetic fallback endpoint.
func (a *App) protectClientRoute(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.ClientAttestationEnabled && strings.HasPrefix(r.URL.Path, "/api/v1/") && !publicDelivery(r) {
			a.runtime.mu.RLock()
			options := a.attestationOptions
			a.runtime.mu.RUnlock()
			attestation.Middleware{Options: options, Next: next}.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
