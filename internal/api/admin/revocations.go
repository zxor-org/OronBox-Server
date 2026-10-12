package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type RevocationEntry struct {
	Fingerprint string `json:"fingerprint"`
	Tag         string `json:"tag,omitempty"`
	Commit      string `json:"commit,omitempty"`
	Reason      string `json:"reason,omitempty"`
	RevokedAt   string `json:"revoked_at,omitempty"`
}

func readRevocations(raw json.RawMessage) ([]RevocationEntry, error) {
	var entries []RevocationEntry
	if json.Unmarshal(raw, &entries) == nil {
		return entries, nil
	}
	var legacy []string
	if json.Unmarshal(raw, &legacy) == nil {
		for _, fp := range legacy {
			entries = append(entries, RevocationEntry{Fingerprint: fp})
		}
	}
	if len(raw) == 0 {
		return []RevocationEntry{}, nil
	}
	return nil, errors.New("invalid auth_revoked_keys JSON")
}

func loadRevocations(s *store.Store, r *http.Request) ([]RevocationEntry, error) {
	var raw json.RawMessage
	err := s.Pool.QueryRow(r.Context(), `SELECT value FROM server_settings WHERE key = 'auth_revoked_keys'`).Scan(&raw)
	if err == pgx.ErrNoRows {
		return []RevocationEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	return readRevocations(raw)
}

func saveRevocations(s *store.Store, r *http.Request, entries []RevocationEntry) error {
	raw, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(r.Context(), `
		INSERT INTO server_settings (key, value, updated_at) VALUES ('auth_revoked_keys', $1, now())
		ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = now()
	`, raw)
	return err
}

func HandleMinimumVersionPut(s *store.Store, refreshRevocation func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}
		var req struct {
			MinimumVersion string `json:"minimum_version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.MinimumVersion) == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}
		raw, _ := json.Marshal(strings.TrimSpace(req.MinimumVersion))
		if _, err := s.Pool.Exec(r.Context(), `INSERT INTO server_settings (key, value, updated_at) VALUES ('auth_minimum_version', $1, now()) ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = now()`, raw); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", "保存失败")
			return
		}
		if refreshRevocation != nil {
			refreshRevocation()
		}
		targetData, _ := json.Marshal(map[string]string{"minimum_version": req.MinimumVersion})
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO audit_logs (actor_uid, action, result, ip, target_data) VALUES ($1, 'update_minimum_version', 'success', $2, $3)`, adminUID, r.RemoteAddr, targetData)
		httputil.JSON(w, http.StatusOK, map[string]string{"minimum_version": req.MinimumVersion})
	}
}

func HandleRevocationsGet(s *store.Store, getMinimumVersion func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		min := ""
		if getMinimumVersion != nil {
			min = getMinimumVersion()
		}
		entries, err := loadRevocations(s, r)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", "读取吊销配置失败")
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]any{"minimum_version": min, "revoked": entries})
	}
}

func HandleRevocationsPost(s *store.Store, refreshRevocation func()) http.HandlerFunc {
	return handleRevocationsPost(s, nil, refreshRevocation)
}

func HandleRevocationsPostWithMaster(s *store.Store, master []byte, refreshRevocation func()) http.HandlerFunc {
	return handleRevocationsPost(s, master, refreshRevocation)
}

func handleRevocationsPost(s *store.Store, master []byte, refreshRevocation func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}
		var req struct {
			Fingerprint string `json:"fingerprint"`
			Tag         string `json:"tag"`
			Commit      string `json:"commit"`
			Reason      string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}
		if len(master) > 0 {
			if req.Tag == "" || req.Commit == "" {
				httputil.Error(w, http.StatusBadRequest, "invalid_body", "缺少 tag/commit")
				return
			}
			mac := hmac.New(sha256.New, master)
			_, _ = mac.Write([]byte("oronbox:v1:" + req.Tag + ":" + req.Commit))
			sum := sha256.Sum256(mac.Sum(nil))
			req.Fingerprint = hex.EncodeToString(sum[:])
		} else if req.Fingerprint == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "缺少 fingerprint")
			return
		}
		var entries []RevocationEntry
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `INSERT INTO server_settings (key, value) VALUES ('auth_revoked_keys', '[]'::jsonb) ON CONFLICT (key) DO NOTHING`)
			if err != nil {
				return err
			}
			var raw json.RawMessage
			if err := tx.QueryRow(r.Context(), `SELECT value FROM server_settings WHERE key = 'auth_revoked_keys' FOR UPDATE`).Scan(&raw); err != nil {
				return err
			}
			var parseErr error
			entries, parseErr = readRevocations(raw)
			if parseErr != nil {
				return parseErr
			}
			found := false
			for i := range entries {
				if entries[i].Fingerprint == req.Fingerprint {
					found = true
					entries[i].Tag, entries[i].Commit, entries[i].Reason = req.Tag, req.Commit, req.Reason
					if entries[i].RevokedAt == "" {
						entries[i].RevokedAt = time.Now().UTC().Format(time.RFC3339)
					}
					break
				}
			}
			if !found {
				entries = append(entries, RevocationEntry{Fingerprint: req.Fingerprint, Tag: req.Tag, Commit: req.Commit, Reason: req.Reason, RevokedAt: time.Now().UTC().Format(time.RFC3339)})
			}
			updatedRaw, err := json.Marshal(entries)
			if err != nil {
				return err
			}
			_, err = tx.Exec(r.Context(), `UPDATE server_settings SET value = $1, updated_at = now() WHERE key = 'auth_revoked_keys'`, updatedRaw)
			return err
		})
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", "保存失败")
			return
		}
		if refreshRevocation != nil {
			refreshRevocation()
		}
		targetData, _ := json.Marshal(req)
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO audit_logs (actor_uid, action, result, ip, target_data) VALUES ($1, 'add_revocation', 'success', $2, $3)`, adminUID, r.RemoteAddr, targetData)
		for _, entry := range entries {
			if entry.Fingerprint == req.Fingerprint {
				httputil.JSON(w, http.StatusCreated, entry)
				return
			}
		}
	}
}

func HandleRevocationsDelete(s *store.Store, refreshRevocation func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		fingerprint := parts[len(parts)-1]
		var entries []RevocationEntry
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			_, err := tx.Exec(r.Context(), `INSERT INTO server_settings (key, value) VALUES ('auth_revoked_keys', '[]'::jsonb) ON CONFLICT (key) DO NOTHING`)
			if err != nil {
				return err
			}
			var raw json.RawMessage
			if err := tx.QueryRow(r.Context(), `SELECT value FROM server_settings WHERE key = 'auth_revoked_keys' FOR UPDATE`).Scan(&raw); err != nil {
				return err
			}
			var parseErr error
			entries, parseErr = readRevocations(raw)
			if parseErr != nil {
				return parseErr
			}
			filtered := entries[:0]
			for _, entry := range entries {
				if entry.Fingerprint != fingerprint {
					filtered = append(filtered, entry)
				}
			}
			updatedRaw, err := json.Marshal(filtered)
			if err != nil {
				return err
			}
			_, err = tx.Exec(r.Context(), `UPDATE server_settings SET value = $1, updated_at = now() WHERE key = 'auth_revoked_keys'`, updatedRaw)
			return err
		})
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", "保存失败")
			return
		}
		if refreshRevocation != nil {
			refreshRevocation()
		}
		targetData, _ := json.Marshal(map[string]string{"fingerprint": fingerprint})
		_, _ = s.Pool.Exec(r.Context(), `INSERT INTO audit_logs (actor_uid, action, result, ip, target_data) VALUES ($1, 'delete_revocation', 'success', $2, $3)`, adminUID, r.RemoteAddr, targetData)
		w.WriteHeader(http.StatusNoContent)
	}
}
