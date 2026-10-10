package admin

import (
	"encoding/json"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type TemplateItem struct {
	ID       int64  `json:"id"`
	Scope    string `json:"scope"`
	Decision string `json:"decision"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Position int    `json:"position"`
	Enabled  bool   `json:"enabled"`
}

// HandleAdminGetTemplates implements GET /admin/api/templates
func HandleAdminGetTemplates(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := s.Pool.Query(r.Context(), `
			SELECT id, scope, decision, title, body, position, enabled
			FROM moderation_reason_templates
			WHERE enabled = true
			ORDER BY position ASC
		`)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []TemplateItem
		for rows.Next() {
			var it TemplateItem
			if err := rows.Scan(&it.ID, &it.Scope, &it.Decision, &it.Title, &it.Body, &it.Position, &it.Enabled); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []TemplateItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleAdminGetRecommendationSettings implements GET /admin/api/settings/recommendation (Spec §14.7)
func HandleAdminGetRecommendationSettings(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		var raw json.RawMessage
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT value FROM server_settings WHERE key = 'recommendation_algorithm_config'
		`).Scan(&raw)

		if len(raw) == 0 {
			raw = json.RawMessage(`{
				"cold_start_days": 14,
				"cold_start_max_boost": 45.0,
				"time_decay_half_days": 45.0,
				"time_decay_exponent": 1.2,
				"weight_coins": 6.0,
				"weight_downloads": 3.0,
				"weight_rating": 2.0,
				"featured_bonus": 20.0,
				"jitter_ratio": 0.20,
				"tag_coefficient_enabled": true
			}`)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// HandleAdminUpdateRecommendationSettings implements PUT /admin/api/settings/recommendation (Spec §14.7)
func HandleAdminUpdateRecommendationSettings(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		var req json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			INSERT INTO server_settings (key, value, updated_at)
			VALUES ('recommendation_algorithm_config', $1, now())
			ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = now()
		`, req)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'update_recommendation_settings', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, req)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// HandleAdminGetAIModeration implements GET /admin/api/settings/ai-moderation (Spec §14.10)
func HandleAdminGetAIModeration(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		var raw json.RawMessage
		_ = s.Pool.QueryRow(r.Context(), `
			SELECT value FROM server_settings WHERE key = 'ai_moderation'
		`).Scan(&raw)

		if len(raw) == 0 {
			raw = json.RawMessage(`{
				"enabled": false,
				"api_url": "https://api.deepseek.com/chat/completions",
				"api_key": "",
				"model": "deepseek-chat",
				"system_prompt": "你是一个严格的手表应用与评论合规审查助手，请检查是否有违规引流、恶意代码或低俗内容"
			}`)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

// HandleAdminUpdateAIModeration implements PUT /admin/api/settings/ai-moderation (Spec §14.10)
func HandleAdminUpdateAIModeration(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		var req json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}

		_, err = s.Pool.Exec(r.Context(), `
			INSERT INTO server_settings (key, value, updated_at)
			VALUES ('ai_moderation', $1, now())
			ON CONFLICT (key) DO UPDATE SET value = $1, updated_at = now()
		`, req)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		_, _ = s.Pool.Exec(r.Context(), `
			INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
			VALUES ($1, 'update_ai_moderation_settings', 'success', $2, $3)
		`, adminUID, r.RemoteAddr, req)

		httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}
