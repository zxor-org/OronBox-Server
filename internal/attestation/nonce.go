package attestation

import (
	"strings"
	"sync"
	"time"
)

// NonceStore is an in-memory single-use nonce cache with TTL (no Redis).
type NonceStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
}

func NewNonceStore(ttl time.Duration) *NonceStore {
	if ttl <= 0 {
		ttl = DefaultSkew
	}
	return &NonceStore{entries: map[string]time.Time{}, ttl: ttl}
}

// Use records the nonce and reports whether it was fresh (true) or replayed (false).
func (s *NonceStore) Use(nonce string, now time.Time) bool {
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.entries {
		if !now.Before(v) {
			delete(s.entries, k)
		}
	}
	if v, ok := s.entries[nonce]; ok && now.Before(v) {
		return false
	}
	s.entries[nonce] = now.Add(s.ttl)
	return true
}
