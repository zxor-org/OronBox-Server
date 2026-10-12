package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
)

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{entries: make(map[string][]time.Time)}
}

func (l *rateLimiter) Allow(key string, limit int, window time.Duration) bool {
	if l == nil || limit <= 0 || window <= 0 {
		return false
	}
	now := time.Now()
	cutoff := now.Add(-window)
	l.mu.Lock()
	defer l.mu.Unlock()
	items := l.entries[key]
	first := 0
	for first < len(items) && items[first].Before(cutoff) {
		first++
	}
	items = items[first:]
	if len(items) >= limit {
		l.entries[key] = items
		return false
	}
	l.entries[key] = append(items, now)
	if len(l.entries) > 10000 {
		for k, values := range l.entries {
			if len(values) == 0 || values[len(values)-1].Before(cutoff) {
				delete(l.entries, k)
			}
		}
	}
	return true
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}

// rateLimitMiddleware applies conservative per-client limits to public API
// traffic. Authentication and attestation still run after this gate; this is
// intentionally an early inexpensive protection against request floods.
func rateLimitMiddleware(app *application, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if app == nil || app.limits == nil || !strings.HasPrefix(r.URL.Path, "/api/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		limit, window := 300, time.Minute
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/auth/"):
			limit = 30
		case r.URL.Path == "/api/v1/blobs" || strings.HasPrefix(r.URL.Path, "/api/v1/creator/"):
			limit = 60
		case strings.HasSuffix(r.URL.Path, "/comments") || strings.HasSuffix(r.URL.Path, "/coins"):
			limit = 60
		}
		// Use a fixed route family rather than the attacker-controlled full
		// path; varying resource IDs must not create fresh buckets indefinitely.
		family := "catalog"
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/auth/"):
			family = "auth"
		case strings.HasPrefix(r.URL.Path, "/api/v1/creator/"):
			family = "creator"
		case r.URL.Path == "/api/v1/blobs":
			family = "upload"
		case r.Method != http.MethodGet && r.Method != http.MethodHead:
			family = "write"
		}
		key := clientIP(r) + "|" + family
		if !app.limits.Allow(key, limit, window) {
			w.Header().Set("Retry-After", "60")
			httputil.Error(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁")
			return
		}
		next.ServeHTTP(w, r)
	})
}
