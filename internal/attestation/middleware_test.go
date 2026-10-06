package attestation

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedHeaders(t *testing.T, master []byte, tag, commit, method, path string, ts time.Time, nonce string, body []byte) map[string]string {
	t.Helper()
	key := DeriveReleaseKey(master, tag, commit)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(CanonicalString(method, path, ts.Unix(), nonce, body)))
	return map[string]string{
		"X-OB-Tag":       tag,
		"X-OB-Commit":    commit,
		"X-OB-Timestamp": strconv.FormatInt(ts.Unix(), 10),
		"X-OB-Nonce":     nonce,
		"X-OB-Signature": base64.RawURLEncoding.EncodeToString(mac.Sum(nil)),
	}
}

func serve(mw Middleware, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/v1/x", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	mw.ServeHTTP(w, req)
	return w
}

func TestMiddlewareAllowsValidAndRejectsBadRequests(t *testing.T) {
	master := make([]byte, 32)
	_, _ = rand.Read(master)
	now := time.Unix(1760000000, 0)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	opts := Options{MasterKey: master, NonceStore: NewNonceStore(time.Minute), Clock: func() time.Time { return now }}
	mw := Middleware{Options: opts, Next: next}
	body := []byte("")

	if w := serve(mw, nil); w.Code != 401 || !strings.Contains(w.Body.String(), "client_attestation_missing") {
		t.Fatalf("missing headers: %d %s", w.Code, w.Body.String())
	}
	if w := serve(mw, signedHeaders(t, master, "v1.1.4", "abc", "GET", "/api/v1/x", now, "n-valid", body)); w.Code != 204 {
		t.Fatalf("valid: %d %s", w.Code, w.Body.String())
	}
	if w := serve(mw, signedHeaders(t, master, "v1.1.4", "abc", "GET", "/api/v1/x", now, "n-valid", body)); w.Code != 401 || !strings.Contains(w.Body.String(), "nonce_replayed") {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}
	if w := serve(mw, signedHeaders(t, master, "v1.1.4", "abc", "GET", "/api/v1/x", now.Add(-time.Hour), "n-exp", body)); w.Code != 401 || !strings.Contains(w.Body.String(), "request_expired") {
		t.Fatalf("expired: %d %s", w.Code, w.Body.String())
	}
	bad := signedHeaders(t, master, "v1.1.4", "abc", "GET", "/api/v1/x", now, "n-bad", body)
	bad["X-OB-Signature"] = base64.RawURLEncoding.EncodeToString([]byte("nope"))
	if w := serve(mw, bad); w.Code != 401 || !strings.Contains(w.Body.String(), "signature_mismatch") {
		t.Fatalf("bad signature: %d %s", w.Code, w.Body.String())
	}

	rev := Middleware{Options: Options{MasterKey: master, NonceStore: NewNonceStore(time.Minute), Clock: func() time.Time { return now }, Revocation: NewRevocation([]string{Fingerprint(DeriveReleaseKey(master, "v1.1.4", "abc"))})}, Next: next}
	if w := serve(rev, signedHeaders(t, master, "v1.1.4", "abc", "GET", "/api/v1/x", now, "n-rev", body)); w.Code != 403 || !strings.Contains(w.Body.String(), "client_version_deprecated") {
		t.Fatalf("revoked: %d %s", w.Code, w.Body.String())
	}
	minMw := Middleware{Options: Options{MasterKey: master, NonceStore: NewNonceStore(time.Minute), Clock: func() time.Time { return now }, MinimumVersion: "9.0.0"}, Next: next}
	if w := serve(minMw, signedHeaders(t, master, "v1.1.4", "abc", "GET", "/api/v1/x", now, "n-min", body)); w.Code != 403 {
		t.Fatalf("min version: %d %s", w.Code, w.Body.String())
	}
}

func TestNonceStoreExpiry(t *testing.T) {
	s := NewNonceStore(time.Minute)
	now := time.Unix(100, 0)
	if !s.Use("a", now) {
		t.Fatal("first use should be fresh")
	}
	if s.Use("a", now.Add(30*time.Second)) {
		t.Fatal("replay within ttl should be rejected")
	}
	if !s.Use("a", now.Add(2*time.Minute)) {
		t.Fatal("reuse after ttl should be fresh")
	}
	if s.Use("", now) {
		t.Fatal("empty nonce should be rejected")
	}
}
