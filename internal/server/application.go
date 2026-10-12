package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/api/catalog"
	"github.com/zxor-org/OronBox-Server/internal/attestation"
	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type application struct {
	cfg         config.Config
	db          *store.Store
	r2          *catalog.R2Client
	started     time.Time
	cipher      *auth.Cipher
	states      sync.Map
	limits      *rateLimiter
	nonces      *attestation.NonceStore
	revMu       sync.RWMutex
	revoked     attestation.Revocation
	minimum     string
	adminAuthFn func(*http.Request) bool
	logger      *slog.Logger
}

func newApplication(d Dependencies) *application {
	c, _ := auth.NewCipher(d.Config.TokenEncryptionKey)
	return &application{
		cfg:     d.Config,
		db:      d.Store,
		r2:      catalog.NewR2Client(d.Config.R2),
		started: d.StartedAt,
		cipher:  c,
		limits:  newRateLimiter(),
		nonces:  attestation.NewNonceStore(d.Config.ClientAttestationSkew),
		revoked: attestation.NewRevocation(nil),
		logger:  slog.Default(),
	}
}

func (a *application) cleanupOAuthStates(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.states.Range(func(key, value any) bool {
				var expiry time.Time
				switch v := value.(type) {
				case time.Time:
					expiry = v
				case interface{ Expiry() time.Time }:
					expiry = v.Expiry()
				}
				if !expiry.IsZero() && !now.Before(expiry) {
					a.states.Delete(key)
				}
				return true
			})
		}
	}
}

func (a *application) withAttestation(next http.Handler) http.Handler {
	if !a.cfg.ClientAttestationEnabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.revMu.RLock()
		rev, min := a.revoked, a.minimum
		a.revMu.RUnlock()
		attestation.Middleware{
			Options: attestation.Options{
				MasterKey:      a.cfg.AttestationMasterKey,
				NonceStore:     a.nonces,
				Skew:           a.cfg.ClientAttestationSkew,
				Revocation:     rev,
				MinimumVersion: min,
			},
			Next: next,
		}.ServeHTTP(w, r)
	})
}

func (a *application) refreshRevocation() {
	if a.db == nil || a.db.Pool == nil {
		return
	}
	// Spec client-attestation-spec.md §6.2: auth_revoked_keys
	var raw json.RawMessage
	if err := a.db.Pool.QueryRow(context.Background(), `
		SELECT value FROM server_settings WHERE key = 'auth_revoked_keys'
	`).Scan(&raw); err == nil {
		var list []string
		if err := json.Unmarshal(raw, &list); err == nil {
			a.revMu.Lock()
			a.revoked = attestation.NewRevocation(list)
			a.revMu.Unlock()
		} else {
			var entries []struct {
				Fingerprint string `json:"fingerprint"`
			}
			if json.Unmarshal(raw, &entries) == nil {
				for _, entry := range entries {
					list = append(list, entry.Fingerprint)
				}
				a.revMu.Lock()
				a.revoked = attestation.NewRevocation(list)
				a.revMu.Unlock()
			}
		}
	}

	// Spec client-attestation-spec.md §6.1 stores a JSON string. Accept the
	// legacy object form as well so an older admin write cannot silently disable
	// the version gate.
	var rawMinimum json.RawMessage
	if err := a.db.Pool.QueryRow(context.Background(), `
		SELECT value FROM server_settings WHERE key = 'auth_minimum_version'
	`).Scan(&rawMinimum); err == nil {
		var minVer string
		if json.Unmarshal(rawMinimum, &minVer) != nil {
			var legacy struct {
				MinimumVersion string `json:"minimum_version"`
			}
			_ = json.Unmarshal(rawMinimum, &legacy)
			minVer = legacy.MinimumVersion
		}
		a.revMu.Lock()
		a.minimum = strings.TrimSpace(minVer)
		a.revMu.Unlock()
	}
}
