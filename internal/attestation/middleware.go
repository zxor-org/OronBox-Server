package attestation

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

// Middleware enforces client build attestation before the wrapped handler.
type Middleware struct {
	Options Options
	Next    http.Handler
}

func (m Middleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tag := r.Header.Get("X-OB-Tag")
	commit := r.Header.Get("X-OB-Commit")
	signature := r.Header.Get("X-OB-Signature")
	timestamp := r.Header.Get("X-OB-Timestamp")
	nonce := r.Header.Get("X-OB-Nonce")
	if tag == "" || commit == "" || signature == "" || timestamp == "" || nonce == "" {
		writeFailure(w, http.StatusUnauthorized, "client_attestation_missing", "missing client attestation headers")
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if e != nil {
		writeFailure(w, http.StatusBadRequest, "invalid_body", "could not read request body")
		return
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	if _, e = Verify(tag, commit, signature, timestamp, nonce, r.Method, r.URL.RequestURI(), body, m.Options); e != nil {
		status, code, message := classify(e)
		writeFailure(w, status, code, message)
		return
	}
	m.Next.ServeHTTP(w, r)
}

func classify(e error) (int, string, string) {
	switch {
	case errors.Is(e, ErrExpired):
		return http.StatusUnauthorized, "request_expired", "request timestamp expired or clock skew too large"
	case errors.Is(e, ErrNonceReplayed):
		return http.StatusUnauthorized, "nonce_replayed", "nonce replayed"
	case errors.Is(e, ErrRevoked), errors.Is(e, ErrVersionDeprecated):
		return http.StatusForbidden, "client_version_deprecated", "client version has been deprecated"
	case errors.Is(e, ErrMissingHeaders):
		return http.StatusUnauthorized, "client_attestation_missing", "missing client attestation headers"
	default:
		return http.StatusUnauthorized, "signature_mismatch", "request signature mismatch"
	}
}

func writeFailure(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `{"code":"`+code+`","message":"`+message+`","detail":{}}`)
}
