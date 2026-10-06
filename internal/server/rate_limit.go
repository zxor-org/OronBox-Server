package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
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
	return true
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
