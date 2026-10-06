package server

import (
	"testing"
	"time"
)

func TestRateLimiterWindow(t *testing.T) {
	l := newRateLimiter()
	if !l.Allow("x", 2, time.Minute) {
		t.Fatal("first call should be allowed")
	}
	if !l.Allow("x", 2, time.Minute) {
		t.Fatal("second call should be allowed")
	}
	if l.Allow("x", 2, time.Minute) {
		t.Fatal("third call should be blocked by rate limit")
	}
	if !l.Allow("y", 1, time.Minute) {
		t.Fatal("keys should be independent")
	}
}
