package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	apiAuth "github.com/zxor-org/OronBox-Server/internal/api/auth"
	"github.com/zxor-org/OronBox-Server/internal/api/catalog"
	"github.com/zxor-org/OronBox-Server/internal/attestation"
	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/config"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
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
	}
}

func (a *application) withAttestation(next http.Handler) http.Handler {
	if !a.cfg.ClientAttestationEnabled {
		return next
	}
	a.revMu.RLock()
	rev, min := a.revoked, a.minimum
	a.revMu.RUnlock()
	return attestation.Middleware{
		Options: attestation.Options{
			MasterKey:      a.cfg.AttestationMasterKey,
			NonceStore:     a.nonces,
			Skew:           a.cfg.ClientAttestationSkew,
			Revocation:     rev,
			MinimumVersion: min,
		},
		Next: next,
	}
}

func (a *application) refreshRevocation() {
	if a.db == nil || a.db.Pool == nil {
		return
	}
	// Spec client-attestation-spec.md §6.2: auth_revoked_keys
	var raw json.RawMessage
	if err := a.db.Pool.QueryRow(context.Background(), `
		SELECT value FROM server_settings WHERE key = 'auth_revoked_keys'
	`).Scan(&raw); err == nil && len(raw) > 0 {
		var list []string
		if err := json.Unmarshal(raw, &list); err == nil {
			a.revMu.Lock()
			a.revoked = attestation.NewRevocation(list)
			a.revMu.Unlock()
		}
	}

	// Spec client-attestation-spec.md §6.1: auth_minimum_version
	var minVer string
	if err := a.db.Pool.QueryRow(context.Background(), `
		SELECT value->>'minimum_version' FROM server_settings WHERE key = 'auth_minimum_version'
	`).Scan(&minVer); err == nil {
		a.revMu.Lock()
		a.minimum = minVer
		a.revMu.Unlock()
	}
}

func (a *application) runCoordinator(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	runOnce := func() {
		if a.db == nil || a.db.Pool == nil {
			return
		}

		// 1. Mark Phase: Reconcile active references across all resource/revision/collection/plugin tables
		markQuery := `
			WITH active_refs AS (
				SELECT icon_sha256 AS sha FROM resources WHERE icon_sha256 <> ''
				UNION ALL
				SELECT cover_sha256 AS sha FROM resources WHERE cover_sha256 <> ''
				UNION ALL
				SELECT jsonb_array_elements_text(preview_shas) AS sha 
				FROM resources 
				WHERE jsonb_typeof(preview_shas) = 'array'
				UNION ALL
				SELECT COALESCE(value->>'blob_sha256', value->>'sha256') AS sha 
				FROM resource_revisions, jsonb_each(downloads) 
				WHERE jsonb_typeof(downloads) = 'object' 
				  AND COALESCE(value->>'blob_sha256', value->>'sha256') IS NOT NULL 
				  AND length(COALESCE(value->>'blob_sha256', value->>'sha256')) = 64
				UNION ALL
				SELECT icon_sha256 AS sha FROM resource_collection_revisions WHERE icon_sha256 <> ''
				UNION ALL
				SELECT cover_sha256 AS sha FROM resource_collection_revisions WHERE cover_sha256 <> ''
				UNION ALL
				SELECT package_sha256 AS sha FROM plugin_versions WHERE package_sha256 <> ''
			),
			ref_counts AS (
				SELECT sha, COUNT(*)::int AS cnt
				FROM active_refs
				WHERE sha IS NOT NULL AND length(sha) = 64
				GROUP BY sha
			)
			UPDATE blobs b
			SET ref_count = COALESCE(rc.cnt, 0)
			FROM (
				SELECT b2.sha256, rc2.cnt
				FROM blobs b2
				LEFT JOIN ref_counts rc2 ON b2.sha256 = rc2.sha
			) rc
			WHERE b.sha256 = rc.sha256
		`
		if _, err := a.db.Pool.Exec(ctx, markQuery); err != nil {
			log.Printf("[coordinator] mark ref_count error: %v", err)
		}

		// 2. Sweep Phase: Find orphaned blobs created > 24 hours ago
		blobDir := a.cfg.BlobStorageDir
		if blobDir == "" {
			blobDir = "/var/lib/oronbox/blobs"
		}

		rows, err := a.db.Pool.Query(ctx, `
			DELETE FROM blobs 
			WHERE ref_count <= 0 AND created_at < now() - interval '24 hours'
			RETURNING sha256
		`)
		if err != nil {
			log.Printf("[coordinator] sweep query error: %v", err)
		} else {
			var deletedSHAs []string
			for rows.Next() {
				var sha string
				if err := rows.Scan(&sha); err == nil {
					deletedSHAs = append(deletedSHAs, sha)
				}
			}
			rows.Close()

			for _, sha := range deletedSHAs {
				path := filepath.Join(blobDir, sha[:2], sha[2:4], sha)
				_ = os.Remove(path)
				altPath := filepath.Join(blobDir, sha)
				_ = os.Remove(altPath)

				if a.r2 != nil && a.r2.IsConfigured() {
					objKey := fmt.Sprintf("%s/%s/%s", sha[:2], sha[2:4], sha)
					_ = a.r2.DeleteObject(ctx, objKey)
				}
			}
		}

		// 3. R2 Replication Sync: Retry pending/failed R2 replicas
		if a.r2 != nil && a.r2.IsConfigured() {
			repRows, err := a.db.Pool.Query(ctx, `
				SELECT b.sha256, b.media_type, b.size_bytes, br.object_key
				FROM blob_replicas br
				JOIN blobs b ON b.sha256 = br.blob_sha256
				WHERE br.backend = 'r2' AND br.state IN ('pending', 'failed') AND br.attempts < 5
				LIMIT 20
			`)
			if err != nil {
				log.Printf("[coordinator] r2 sync query error: %v", err)
			} else {
				type pendingRep struct {
					sha   string
					mtype string
					size  int64
					key   string
				}
				var pending []pendingRep
				for repRows.Next() {
					var p pendingRep
					if err := repRows.Scan(&p.sha, &p.mtype, &p.size, &p.key); err == nil {
						pending = append(pending, p)
					}
				}
				repRows.Close()

				for _, p := range pending {
					localPath := filepath.Join(blobDir, p.sha[:2], p.sha[2:4], p.sha)
					if _, err := os.Stat(localPath); os.IsNotExist(err) {
						localPath = filepath.Join(blobDir, p.sha)
					}
					f, err := os.Open(localPath)
					if err != nil {
						_, _ = a.db.Pool.Exec(ctx, `
							UPDATE blob_replicas
							SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now()
							WHERE blob_sha256 = $1 AND backend = 'r2'
						`, p.sha, err.Error())
						continue
					}
					err = a.r2.PutObject(ctx, p.key, p.mtype, f, p.size, p.sha)
					f.Close()
					if err != nil {
						_, _ = a.db.Pool.Exec(ctx, `
							UPDATE blob_replicas
							SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now()
							WHERE blob_sha256 = $1 AND backend = 'r2'
						`, p.sha, err.Error())
					} else {
						_, _ = a.db.Pool.Exec(ctx, `
							UPDATE blob_replicas
							SET state = 'ready', error_message = '', updated_at = now()
							WHERE blob_sha256 = $1 AND backend = 'r2'
						`, p.sha)
					}
				}
			}
		}
	}

	runOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}

func (a *application) finishAdminLogin(w http.ResponseWriter, r *http.Request, code string) {
	authSvc := apiAuth.NewService(a.cfg, a.db, a.cipher, &a.states)
	uid, username, avatar, accessToken, err := authSvc.FetchBandBBSUserWithToken(r.Context(), code, a.cfg.BandBBS.RedirectURI)
	if err != nil {
		a.renderConsoleError(w, r, err.Error(), "返回登录", "/console")
		return
	}

	if accessToken != "" && a.cfg.BandBBS.RevokeURL != "" {
		go func(tok string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = authSvc.RevokeBandBBSToken(ctx, tok)
		}(accessToken)
	}

	allowed := false
	for _, v := range a.cfg.AdminBandBBSUIDs {
		if v == uid {
			allowed = true
			break
		}
	}
	if !allowed {
		a.renderConsoleError(w, r, "该账号未被授权管理员权限", "返回登录", "/console")
		return
	}

	intUID, _ := strconv.Atoi(uid)
	_, _ = a.db.Pool.Exec(r.Context(), `
		INSERT INTO users (bandbbs_uid, username, avatar_url, role, updated_at)
		VALUES ($1, $2, $3, 'admin', now())
		ON CONFLICT (bandbbs_uid) DO UPDATE 
		SET username = EXCLUDED.username, avatar_url = EXCLUDED.avatar_url, role = 'admin', updated_at = now()
	`, intUID, username, avatar)

	sessBytes := make([]byte, 32)
	_, _ = rand.Read(sessBytes)
	sessToken := hex.EncodeToString(sessBytes)
	sessHash := sha256.Sum256([]byte(sessToken))

	_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM admin_sessions WHERE user_uid = $1`, intUID)
	_, err = a.db.Pool.Exec(r.Context(), `
		INSERT INTO admin_sessions (session_hash, user_uid, username, ip, expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '24 hours')
	`, sessHash[:], intUID, username, r.RemoteAddr)

	if err != nil {
		a.renderConsoleError(w, r, "创建管理会话失败", "重试", "/console")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "oronbox_admin",
		Value:    sessToken,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/console/", http.StatusFound)
}

func (a *application) isAdminAuthenticated(r *http.Request) bool {
	if a.adminAuthFn != nil {
		return a.adminAuthFn(r)
	}
	cookie, err := r.Cookie("oronbox_admin")
	if err != nil || cookie.Value == "" || a.db == nil || a.db.Pool == nil {
		return false
	}
	hash := sha256.Sum256([]byte(cookie.Value))
	var uid int
	err = a.db.Pool.QueryRow(r.Context(), `
		SELECT user_uid FROM admin_sessions WHERE session_hash = $1 AND expires_at > now()
	`, hash[:]).Scan(&uid)
	return err == nil && uid > 0
}

func (a *application) adminAuthorize(w http.ResponseWriter, r *http.Request) {
	stateBytes := make([]byte, 16)
	_, _ = rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)
	a.states.Store("admin_"+state, time.Now().Add(10*time.Minute))

	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=admin_%s",
		a.cfg.BandBBS.AuthorizeURL,
		url.QueryEscape(a.cfg.BandBBS.ClientID),
		url.QueryEscape(a.cfg.BandBBS.RedirectURI),
		url.QueryEscape(a.cfg.BandBBS.Scopes),
		url.QueryEscape(state),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (a *application) adminLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("oronbox_admin")
	if err == nil && cookie.Value != "" {
		hash := sha256.Sum256([]byte(cookie.Value))
		_, _ = a.db.Pool.Exec(r.Context(), `DELETE FROM admin_sessions WHERE session_hash = $1`, hash[:])
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oronbox_admin",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (a *application) adminSession(w http.ResponseWriter, r *http.Request) {
	uid, username, err := httputil.AuthenticateAdmin(r.Context(), a.db, r)
	if err != nil || uid <= 0 {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"uid":           uid,
		"username":      username,
	})
}

func (a *application) adminRefresh(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "refreshed"})
}

func (a *application) releaseSync(w http.ResponseWriter, r *http.Request) {
	httputil.JSON(w, http.StatusOK, map[string]string{"status": "synced"})
}
