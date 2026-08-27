package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

const (
	adminCSRFField  = web.CSRFFieldName
	adminCSRFHeader = web.CSRFHeaderName
)

// adminCSRFToken derives a token from the admin session id and the server
// session secret. Deriving instead of storing keeps the token stateless while
// still binding it to exactly one session, so a token captured from one
// administrator cannot be replayed against another.
func (a *App) adminCSRFToken(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	digest := authcore.HashToken("admin-csrf\x00"+sessionID, a.cfg.SessionSecret)
	return base64.RawURLEncoding.EncodeToString(digest)
}

// adminCSRFTokenFor resolves the token for the session attached to a request,
// returning an empty string outside authenticated admin handlers.
func (a *App) adminCSRFTokenFor(r *http.Request) string {
	if r == nil {
		return ""
	}
	session, ok := r.Context().Value(adminContextKey{}).(store.AdminSession)
	if !ok {
		return ""
	}
	return a.adminCSRFToken(session.ID)
}

// adminCSRFAccepted verifies the token carried by an unsafe admin request. The
// hidden form field covers server-rendered forms and the header covers
// script-driven requests; both are bound to the same session token.
func (a *App) adminCSRFAccepted(r *http.Request, sessionID string) bool {
	expected := a.adminCSRFToken(sessionID)
	if expected == "" {
		return false
	}
	presented := strings.TrimSpace(r.Header.Get(adminCSRFHeader))
	if presented == "" {
		presented = strings.TrimSpace(r.PostFormValue(adminCSRFField))
	}
	return presented != "" && constantTimeEqual(presented, expected)
}

// isMultipartUpload reports whether the body may only be read with an explicit
// size budget. The middleware skips those requests so it never buffers an
// upload before the owning handler has capped it; parseAdminUpload runs the
// token check instead.
func isMultipartUpload(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && strings.HasPrefix(mediaType, "multipart/")
}

// parseAdminUpload caps, parses and CSRF-checks a multipart admin upload. It is
// the only way to reach the file in these handlers, which is what keeps the
// deferred token check from being forgotten.
func (a *App) parseAdminUpload(w http.ResponseWriter, r *http.Request, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(1<<20))
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		return fmt.Errorf("invalid upload: %w", err)
	}
	if !a.adminCSRFAccepted(r, currentAdmin(r).ID) {
		return errAdminCSRF
	}
	return nil
}

var errAdminCSRF = errors.New("admin request rejected")

// rejectAdminUpload maps a parseAdminUpload failure onto a response, keeping
// the CSRF rejection a 403 and everything else a 400.
func (a *App) rejectAdminUpload(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errAdminCSRF) {
		_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "admin_csrf_rejected", "failure", a.clientIP(r), r.UserAgent(), "path="+r.URL.Path+" reason=missing_csrf_token")
		http.Error(w, "admin request rejected", http.StatusForbidden)
		return
	}
	http.Error(w, "invalid upload", http.StatusBadRequest)
}
