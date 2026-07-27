package server

import (
	"sync"
	"time"
)

// ipRateLimiter is a fixed-window per-key counter. It backs the download
// signing endpoint, where a database-backed limiter would be needlessly
// expensive.
type ipRateLimiter struct {
	mu      sync.Mutex
	limit   int
	windows map[string]*rateWindow
}

type rateWindow struct {
	start time.Time
	count int
}

func newIPRateLimiter(limitPerMinute int) *ipRateLimiter {
	if limitPerMinute <= 0 {
		limitPerMinute = 30
	}
	return &ipRateLimiter{limit: limitPerMinute, windows: map[string]*rateWindow{}}
}

func (limiter *ipRateLimiter) allow(key string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	now := time.Now()
	window, ok := limiter.windows[key]
	if !ok || now.Sub(window.start) >= time.Minute {
		window = &rateWindow{start: now}
		limiter.windows[key] = window
	}
	// Evict stale windows while we hold the lock so the map cannot grow
	// without bound under rotating IPs.
	if len(limiter.windows) > 4096 {
		for candidate, entry := range limiter.windows {
			if now.Sub(entry.start) >= 2*time.Minute {
				delete(limiter.windows, candidate)
			}
		}
	}
	if window.count >= limiter.limit {
		return false
	}
	window.count++
	return true
}
