package server

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/observability"
)

// ipRateLimiter is a fixed-window per-key counter. It backs the download
// signing and credential endpoints, where a database-backed limiter would be
// needlessly expensive.
type ipRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	windows map[string]*rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

func newIPRateLimiter(limitPerMinute int) *ipRateLimiter {
	return newWindowLimiter(limitPerMinute, time.Minute, 30)
}

func newWindowLimiter(limit int, window time.Duration, fallbackLimit int) *ipRateLimiter {
	if limit <= 0 {
		limit = fallbackLimit
	}
	if window <= 0 {
		window = time.Minute
	}
	return &ipRateLimiter{limit: limit, window: window, windows: map[string]*rateWindow{}}
}

func (limiter *ipRateLimiter) allow(key string) bool {
	return limiter.record(key, true)
}

// observe counts an event against the budget without reporting whether the
// caller may proceed, so failure budgets can be charged after the fact.
func (limiter *ipRateLimiter) observe(key string) {
	limiter.record(key, true)
}

// exceeded reports whether a key already burned its budget, leaving the
// counter untouched.
func (limiter *ipRateLimiter) exceeded(key string) bool {
	return !limiter.record(key, false)
}

func (limiter *ipRateLimiter) record(key string, consume bool) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := time.Now()
	window, ok := limiter.windows[key]
	if !ok || now.Sub(window.start) >= limiter.window {
		window = &rateWindow{start: now}
		limiter.windows[key] = window
	}
	// Evict stale windows while we hold the lock so the map cannot grow
	// without bound under rotating IPs.
	if len(limiter.windows) > 4096 {
		for candidate, entry := range limiter.windows {
			if now.Sub(entry.start) >= 2*limiter.window {
				delete(limiter.windows, candidate)
			}
		}
	}
	if window.count >= limiter.limit {
		return false
	}
	if consume {
		window.count++
	}
	return true
}

// statusRecorder remembers the status a handler wrote so a wrapper can tell a
// rejected credential from an accepted one without re-reading the body.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(payload []byte) (int, error) {
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	return recorder.ResponseWriter.Write(payload)
}

// throttleCredentials caps the credential endpoints on two axes. The attempt
// ceiling stays generous so a shared NAT egress keeps working; the much smaller
// failure budget is what actually ends an online guessing run, because a client
// with valid credentials never touches it.
func (a *App) throttleCredentials(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := a.clientIP(r)
		if reason := a.credentialThrottleReason(key); reason != "" {
			observability.From(r.Context()).With("component", "auth").Warn(
				"credential request throttled",
				"reason", reason,
				"path", r.URL.Path,
			)
			retryAfter := a.cfg.Limits.AuthFailureWindow
			if reason == "attempt_rate" {
				retryAfter = time.Minute
			}
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			writeJSON(w, http.StatusTooManyRequests, errorBody("rate_limited", "too many attempts, please retry later"))
			return
		}
		recorder := &statusRecorder{ResponseWriter: w}
		next(recorder, r)
		if recorder.status == http.StatusBadRequest || recorder.status == http.StatusUnauthorized {
			a.authFailures.observe(key)
		}
	}
}

func (a *App) allowPublicBlobDownload(ip string, artifact bool) bool {
	if !artifact {
		return true
	}
	return a.downloadLimiter.allow(ip)
}

func (a *App) credentialThrottleReason(key string) string {
	if a.authFailures.exceeded(key) {
		return "failure_budget"
	}
	if !a.authAttempts.allow(key) {
		return "attempt_rate"
	}
	return ""
}
