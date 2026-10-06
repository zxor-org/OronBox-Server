package attestation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultSkew      = 300 * time.Second
	derivationPrefix = "oronbox:v1:"
)

var (
	ErrMissingHeaders    = errors.New("missing client attestation headers")
	ErrInvalidSignature  = errors.New("client request signature is invalid")
	ErrExpired           = errors.New("client request timestamp is outside the accepted window")
	ErrNonceReplayed     = errors.New("client request nonce has already been used")
	ErrRevoked           = errors.New("client version has been revoked")
	ErrVersionDeprecated = errors.New("client version is below the server minimum")
)

// DeriveReleaseKey returns OB_RELEASE_KEY = HMAC-SHA256(master, "oronbox:v1:"+tag+":"+commit).
// The derived key never leaves the process and is recomputed server-side per request.
func DeriveReleaseKey(master []byte, tag, commit string) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(derivationPrefix + tag + ":" + commit))
	return mac.Sum(nil)
}

// Fingerprint returns HEX(SHA256(key)), the identity used by the revocation blacklist.
func Fingerprint(key []byte) string {
	h := sha256.Sum256(key)
	return hex.EncodeToString(h[:])
}

// CanonicalString is the exact byte string both client and server sign.
func CanonicalString(method, path string, timestamp int64, nonce string, body []byte) string {
	h := sha256.Sum256(body)
	return strings.ToUpper(method) + "\n" + path + "\n" + strconv.FormatInt(timestamp, 10) + "\n" + nonce + "\n" + hex.EncodeToString(h[:])
}

// CompareVersions compares dotted semver-ish versions; returns -1, 0 or 1.
func CompareVersions(a, b string) int {
	clean := func(v string) []int {
		v = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(v)), "v")
		v = strings.SplitN(v, "+", 2)[0]
		v = strings.SplitN(v, "-", 2)[0]
		ps := strings.Split(v, ".")
		o := make([]int, len(ps))
		for i, p := range ps {
			o[i], _ = strconv.Atoi(p)
		}
		return o
	}
	x, y := clean(a), clean(b)
	for i := 0; i < len(x) || i < len(y); i++ {
		p, q := 0, 0
		if i < len(x) {
			p = x[i]
		}
		if i < len(y) {
			q = y[i]
		}
		if p < q {
			return -1
		}
		if p > q {
			return 1
		}
	}
	return 0
}

// Options configures request verification.
type Options struct {
	MasterKey      []byte
	NonceStore     *NonceStore
	Clock          func() time.Time
	Skew           time.Duration
	MinimumVersion string
	Revocation     Revocation
}

func (o Options) now() time.Time {
	if o.Clock != nil {
		return o.Clock()
	}
	return time.Now()
}

// Verify validates the five attestation headers and the request HMAC, returning the
// authenticated build tag. Order: headers -> clock -> signature -> revocation -> nonce.
func Verify(tag, commit, signature, timestamp, nonce, method, path string, body []byte, o Options) (string, error) {
	if tag == "" || commit == "" || signature == "" || timestamp == "" || nonce == "" {
		return "", ErrMissingHeaders
	}
	if len(o.MasterKey) == 0 {
		return "", ErrMissingHeaders
	}
	if o.Skew <= 0 {
		o.Skew = DefaultSkew
	}
	ts, e := strconv.ParseInt(strings.TrimSpace(timestamp), 10, 64)
	if e != nil {
		return "", ErrExpired
	}
	now := o.now()
	if d := now.Sub(time.Unix(ts, 0)); d > o.Skew || d < -o.Skew {
		return "", ErrExpired
	}
	key := DeriveReleaseKey(o.MasterKey, tag, commit)
	sig, e := base64.RawURLEncoding.DecodeString(strings.TrimSpace(signature))
	if e != nil {
		sig, e = base64.URLEncoding.DecodeString(strings.TrimSpace(signature))
	}
	if e != nil {
		return "", ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(CanonicalString(method, path, ts, nonce, body)))
	if !hmac.Equal(mac.Sum(nil), sig) {
		return "", ErrInvalidSignature
	}
	// Signature is valid, so tag/commit are trustworthy: apply revocation now.
	if o.Revocation.IsRevoked(Fingerprint(key)) {
		return "", ErrRevoked
	}
	if o.MinimumVersion != "" && CompareVersions(tag, o.MinimumVersion) < 0 {
		return "", ErrVersionDeprecated
	}
	if o.NonceStore == nil || !o.NonceStore.Use(nonce, now) {
		return "", ErrNonceReplayed
	}
	return tag, nil
}
