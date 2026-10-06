package attestation

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"
)

func TestDeriveAndRequestVerification(t *testing.T) {
	master := make([]byte, 32)
	_, _ = rand.Read(master)
	tag, commit := "v1.1.4", "d7a5b3f0e1c2a9b8c7d6e5f4a3b2c1d0e9f8a7b6"
	key := DeriveReleaseKey(master, tag, commit)
	now := time.Unix(1760000000, 0)
	body := []byte(`{"ok":true}`)
	nonce := "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	sign := func(n string) string {
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(CanonicalString("POST", "/api/v1/coins", now.Unix(), n, body)))
		return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	sig := sign(nonce)
	opts := Options{MasterKey: master, Clock: func() time.Time { return now }, NonceStore: NewNonceStore(time.Minute)}
	if _, err := Verify(tag, commit, sig, "1760000000", nonce, "POST", "/api/v1/coins", body, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(tag, commit, sig, "1760000000", nonce, "POST", "/api/v1/coins", body, opts); err != ErrNonceReplayed {
		t.Fatalf("replay error=%v", err)
	}
	opts.Revocation = NewRevocation([]string{Fingerprint(key)})
	if _, err := Verify(tag, commit, sign("other-nonce"), "1760000000", "other-nonce", "POST", "/api/v1/coins", body, opts); err != ErrRevoked {
		t.Fatalf("revoked error=%v", err)
	}
}

func TestVerifyRejectsWrongKeyAndExpired(t *testing.T) {
	master := make([]byte, 32)
	_, _ = rand.Read(master)
	body := []byte("")
	now := time.Unix(1760000000, 0)
	bad := base64.RawURLEncoding.EncodeToString([]byte("not-a-mac"))
	opts := Options{MasterKey: master, Clock: func() time.Time { return now }, NonceStore: NewNonceStore(time.Minute)}
	if _, err := Verify("v1", "abc", bad, "1760000000", "n1", "GET", "/x", body, opts); err != ErrInvalidSignature {
		t.Fatalf("want signature_mismatch, got %v", err)
	}
	if _, err := Verify("v1", "abc", bad, "1", "n1", "GET", "/x", body, opts); err != ErrExpired {
		t.Fatalf("want expired, got %v", err)
	}
	if _, err := Verify("", "abc", bad, "1760000000", "n1", "GET", "/x", body, opts); err != ErrMissingHeaders {
		t.Fatalf("want missing headers, got %v", err)
	}
}

func TestCompareVersions(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{{"1.2.0", "1.10.0", -1}, {"v2.0.0", "2.0", 0}, {"3.0.0-beta", "2.9.9", 1}, {"1.10.0", "1.9.0", 1}, {"2.0.0+build5", "2.0.0", 0}} {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("%s/%s=%d", tt.a, tt.b, got)
		}
	}
}
