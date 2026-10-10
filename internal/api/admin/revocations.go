package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// HandleMinimumVersionPut 严格写入 auth_minimum_version (对齐 client-attestation-spec.md §6.1)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误")
			return
		}
		raw, _ := json.Marshal(map[string]string{"minimum_version": req.MinimumVersion})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO server_settings (key, value, updated_at)
			VALUES ('auth_minimum_version', $1, now())
			ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = now()
		`, raw)
		if refreshRevocation != nil {
			refreshRevocation()
		}

		targetData, _ := json.Marshal(map[string]any{"minimum_version": req.MinimumVersion})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'update_minimum_version', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// HandleRevocationsGet 严格返回 {minimum_version, revoked:[...]}
func HandleRevocationsGet(s *store.Store, getMinimumVersion func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		min := ""
		if getMinimumVersion != nil {
			min = getMinimumVersion()
		}

		var raw json.RawMessage
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT value FROM server_settings WHERE key = 'auth_revoked_keys'
		`).Scan(&raw)

		var list []string
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &list)
		}
		if list == nil {
			list = []string{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"minimum_version": min,
			"revoked":         list,
		})
	}
}

// HandleRevocationsPost 严格落库至 auth_revoked_keys
func HandleRevocationsPost(s *store.Store, refreshRevocation func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		var req struct {
			Fingerprint string `json:"fingerprint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Fingerprint == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数错误，缺少 fingerprint")
			return
		}

		var raw json.RawMessage
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT value FROM server_settings WHERE key = 'auth_revoked_keys'
		`).Scan(&raw)

		var list []string
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &list)
		}
		found := false
		for _, fp := range list {
			if fp == req.Fingerprint {
				found = true
				break
			}
		}
		if !found {
			list = append(list, req.Fingerprint)
		}

		updatedRaw, _ := json.Marshal(list)
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO server_settings (key, value, updated_at)
			VALUES ('auth_revoked_keys', $1, now())
			ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = now()
		`, updatedRaw)

		if refreshRevocation != nil {
			refreshRevocation()
		}

		targetData, _ := json.Marshal(map[string]any{"fingerprint": req.Fingerprint})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'add_revocation', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "added"})
	}
}

// HandleRevocationsDelete 严格从 auth_revoked_keys 移除
func HandleRevocationsDelete(s *store.Store, refreshRevocation func()) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		fingerprint := parts[len(parts)-1]
		if fingerprint == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_fingerprint", "缺少指纹参数")
			return
		}

		var raw json.RawMessage
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT value FROM server_settings WHERE key = 'auth_revoked_keys'
		`).Scan(&raw)

		var list []string
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &list)
		}

		newList := make([]string, 0, len(list))
		for _, fp := range list {
			if fp != fingerprint {
				newList = append(newList, fp)
			}
		}

		updatedRaw, _ := json.Marshal(newList)
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO server_settings (key, value, updated_at)
			VALUES ('auth_revoked_keys', $1, now())
			ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = now()
		`, updatedRaw)

		if refreshRevocation != nil {
			refreshRevocation()
		}

		targetData, _ := json.Marshal(map[string]any{"fingerprint": fingerprint})
		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'delete_revocation', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, targetData)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}
