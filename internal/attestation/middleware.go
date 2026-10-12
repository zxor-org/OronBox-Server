package attestation

import (
	"bytes"
	"errors"
	"io"
	"net/http"
)

// Body verification buffers the request because the signature covers the full
// payload. Keep a bounded pool for uploads, while allowing ordinary bodyless
// GET requests to proceed independently.
var attestationBodySlots = make(chan struct{}, 8)

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
	if r.Body != nil && !(r.Method == http.MethodGet && r.ContentLength == 0) && !(r.Method == http.MethodHead && r.ContentLength == 0) {
		select {
		case attestationBodySlots <- struct{}{}:
			defer func() { <-attestationBodySlots }()
		default:
			writeFailure(w, http.StatusTooManyRequests, "attestation_busy", "too many attested uploads")
			return
		}
	}
	// Keep the signed-body ceiling aligned with the blob upload ceiling.
	const maxSignedBody = 100 << 20
	if r.ContentLength > maxSignedBody {
		writeFailure(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
		return
	}
	reader := io.Reader(http.NoBody)
	if r.Body != nil {
		reader = r.Body
	}
	body, e := io.ReadAll(io.LimitReader(reader, maxSignedBody+1))
	if e != nil {
		writeFailure(w, http.StatusBadRequest, "invalid_body", "could not read request body")
		return
	}
	if len(body) > maxSignedBody {
		writeFailure(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large")
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
