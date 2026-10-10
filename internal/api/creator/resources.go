package creator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type CreatorSubmitRequest struct {
	ResourceID  string          `json:"resource_id"`
	Title       string          `json:"title"`
	Summary     string          `json:"summary"`
	Description string          `json:"description"`
	Restype     string          `json:"restype"`
	PaidType    string          `json:"paid_type"`
	Version     string          `json:"version"`
	Changelog   string          `json:"changelog"`
	IconSHA256  string          `json:"icon_sha256"`
	CoverSHA256 string          `json:"cover_sha256"`
	PreviewSHAs []string        `json:"preview_shas"`
	Downloads   json.RawMessage `json:"downloads"`
	Tags        []string        `json:"tags"`
}

type CreatorResourceItem struct {
	ResourceID      string  `json:"resource_id"`
	Title           string  `json:"title"`
	Summary         string  `json:"summary"`
	Restype         string  `json:"restype"`
	ModerationState string  `json:"moderation_state"`
	CurrentVersion  string  `json:"current_version,omitempty"`
	PendingVersion  string  `json:"pending_version,omitempty"`
	DownloadCount   int     `json:"download_count"`
	CoinCount       int     `json:"coin_count"`
	Rating          float64 `json:"rating"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// HandleCreatorGetResources implements GET /api/v1/creator/resources (Spec §12.1)
func HandleCreatorGetResources(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT r.resource_id, r.title, r.summary, r.restype, r.moderation_state,
			       COALESCE(cur_rev.version, ''),
			       COALESCE(pend_rev.version, ''),
			       r.download_count, r.coin_count, r.rating,
			       to_char(r.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       to_char(r.updated_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
			FROM resources r
			LEFT JOIN resource_revisions cur_rev ON r.current_revision_id = cur_rev.id
			LEFT JOIN LATERAL (
				SELECT version FROM resource_revisions 
				WHERE resource_id = r.resource_id AND state IN ('waiting_review', 'draft', 'changes_requested')
				ORDER BY id DESC LIMIT 1
			) pend_rev ON true
			WHERE r.owner_uid = $1
			   OR EXISTS (SELECT 1 FROM resource_collaborators rc WHERE rc.resource_id = r.resource_id AND rc.user_uid = $1 AND rc.status = 'accepted')
			ORDER BY r.updated_at DESC
		`, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []CreatorResourceItem
		for rows.Next() {
			var it CreatorResourceItem
			if err := rows.Scan(&it.ResourceID, &it.Title, &it.Summary, &it.Restype, &it.ModerationState,
				&it.CurrentVersion, &it.PendingVersion, &it.DownloadCount, &it.CoinCount, &it.Rating,
				&it.CreatedAt, &it.UpdatedAt); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []CreatorResourceItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleCreatorCreateResource implements POST /api/v1/creator/resources (Spec §12.2)
func HandleCreatorCreateResource(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var req struct {
			ResourceID string `json:"resource_id"`
			Title      string `json:"title"`
			Summary    string `json:"summary"`
			Restype    string `json:"restype"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}
		req.ResourceID = strings.TrimSpace(req.ResourceID)
		req.Title = strings.TrimSpace(req.Title)
		if req.ResourceID == "" || req.Title == "" {
			httputil.Error(w, http.StatusBadRequest, "missing_fields", "缺少 resource_id 或 title")
			return
		}
		if req.Restype == "" {
			req.Restype = "quick_app"
		}

		authorsJSON, _ := json.Marshal([]map[string]any{{"uid": userUID}})
		_, err = s.Pool.Exec(r.Context(), `
			INSERT INTO resources (resource_id, title, summary, restype, owner_uid, authors, moderation_state)
			VALUES ($1, $2, $3, $4, $5, $6, 'suspended')
		`, req.ResourceID, req.Title, req.Summary, req.Restype, userUID, authorsJSON)

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "create_failed", "资源ID已存在或创建失败")
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"resource_id": req.ResourceID,
			"status":      "created",
		})
	}
}

// HandleCreatorSaveDraft implements PUT /api/v1/creator/resources/:id/draft (Spec §12.3)
func HandleCreatorSaveDraft(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		resID := r.PathValue("id")
		if resID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "resources" && idx+1 < len(parts) {
					resID = parts[idx+1]
					break
				}
			}
		}

		var req struct {
			Version     string          `json:"version"`
			Name        string          `json:"name"`
			Summary     string          `json:"summary"`
			Changelog   string          `json:"changelog"`
			Downloads   json.RawMessage `json:"downloads"`
			IconSHA256  string          `json:"icon_sha256"`
			CoverSHA256 string          `json:"cover_sha256"`
			PreviewSHAs []string        `json:"preview_shas"`
			Tags        []string        `json:"tags"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}

		var revID int64
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Check ownership
			var ownerUID int
			if err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, resID).Scan(&ownerUID); err != nil {
				return err
			}
			if ownerUID != userUID {
				var allowed bool
				_ = tx.QueryRow(r.Context(), `
					SELECT true FROM resource_collaborators WHERE resource_id = $1 AND user_uid = $2 AND status = 'accepted'
				`, resID, userUID).Scan(&allowed)
				if !allowed {
					return fmt.Errorf("forbidden")
				}
			}

			// Update resource metadata if provided
			if req.IconSHA256 != "" || req.CoverSHA256 != "" || len(req.Tags) > 0 {
				tagsJSON, _ := json.Marshal(req.Tags)
				previewsJSON, _ := json.Marshal(req.PreviewSHAs)
				_, _ = tx.Exec(r.Context(), `
					UPDATE resources 
					SET icon_sha256 = COALESCE(NULLIF($1, ''), icon_sha256),
					    cover_sha256 = COALESCE(NULLIF($2, ''), cover_sha256),
					    preview_shas = CASE WHEN $3 = '[]'::jsonb THEN preview_shas ELSE $3 END,
					    tags = CASE WHEN $4 = '[]'::jsonb THEN tags ELSE $4 END,
					    updated_at = now()
					WHERE resource_id = $5
				`, req.IconSHA256, req.CoverSHA256, previewsJSON, tagsJSON, resID)
			}

			if req.Version == "" {
				req.Version = "1.0.0"
			}
			if req.Downloads == nil {
				req.Downloads = json.RawMessage("{}")
			}

			// Insert draft revision
			return tx.QueryRow(r.Context(), `
				INSERT INTO resource_revisions (resource_id, version, name, summary, changelog, downloads, state, creator_uid)
				VALUES ($1, $2, $3, $4, $5, $6, 'draft', $7)
				ON CONFLICT (resource_id, version) DO UPDATE
				SET name = EXCLUDED.name, summary = EXCLUDED.summary, changelog = EXCLUDED.changelog,
				    downloads = EXCLUDED.downloads, state = 'draft', updated_at = now()
				RETURNING id
			`, resID, req.Version, req.Name, req.Summary, req.Changelog, req.Downloads, userUID).Scan(&revID)
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "save_draft_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":      "draft_saved",
			"revision_id": revID,
		})
	}
}

// HandleCreatorPublishResource implements POST /api/v1/creator/resources/:id/publish (Spec §12.4)
func HandleCreatorPublishResource(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		resID := r.PathValue("id")
		if resID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "resources" && idx+1 < len(parts) {
					resID = parts[idx+1]
					break
				}
			}
		}

		var req struct {
			Targets     []string        `json:"targets"`
			Summary     string          `json:"summary"`
			Description string          `json:"description"`
			Config      json.RawMessage `json:"config"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		var revID int64
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			var ownerUID int
			if err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, resID).Scan(&ownerUID); err != nil {
				return err
			}
			if ownerUID != userUID {
				var allowed bool
				_ = tx.QueryRow(r.Context(), `
					SELECT true FROM resource_collaborators WHERE resource_id = $1 AND user_uid = $2 AND status = 'accepted'
				`, resID, userUID).Scan(&allowed)
				if !allowed {
					return fmt.Errorf("forbidden")
				}
			}

			// Find latest draft or changes_requested revision
			err := tx.QueryRow(r.Context(), `
				SELECT id FROM resource_revisions 
				WHERE resource_id = $1 AND state IN ('draft', 'changes_requested')
				ORDER BY id DESC LIMIT 1
			`, resID).Scan(&revID)
			if err != nil {
				return fmt.Errorf("no_revisable_version")
			}

			if req.Summary != "" || req.Description != "" {
				_, _ = tx.Exec(r.Context(), `
					UPDATE resources 
					SET summary = COALESCE(NULLIF($1, ''), summary),
					    description = COALESCE(NULLIF($2, ''), description),
					    updated_at = now()
					WHERE resource_id = $3
				`, req.Summary, req.Description, resID)
			}

			_, err = tx.Exec(r.Context(), `
				UPDATE resource_revisions 
				SET state = 'waiting_review', config = COALESCE($1, config), updated_at = now()
				WHERE id = $2
			`, req.Config, revID)
			if err != nil {
				return err
			}

			// Create review case
			_, err = tx.Exec(r.Context(), `
				INSERT INTO review_cases (revision_id, state)
				VALUES ($1, 'pending')
				ON CONFLICT (revision_id) DO UPDATE SET state = 'pending', updated_at = now()
			`, revID)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "publish_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":      "waiting_review",
			"revision_id": revID,
			"message":     "提审已成功提交审核",
		})
	}
}

// HandleCreatorSubmit implements POST /api/v1/creator/submissions (Compatibility alias)
func HandleCreatorSubmit(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var req CreatorSubmitRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "请求体JSON格式错误")
			return
		}

		req.ResourceID = strings.TrimSpace(req.ResourceID)
		req.Title = strings.TrimSpace(req.Title)
		req.Version = strings.TrimSpace(req.Version)
		if req.ResourceID == "" || req.Title == "" || req.Version == "" {
			httputil.Error(w, http.StatusBadRequest, "missing_fields", "缺少必要参数 resource_id/title/version")
			return
		}

		var revID int64
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			var ownerUID int
			err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resources WHERE resource_id = $1`, req.ResourceID).Scan(&ownerUID)

			authorsJSON, _ := json.Marshal([]map[string]any{{"uid": userUID}})
			tagsJSON, _ := json.Marshal(req.Tags)
			previewsJSON, _ := json.Marshal(req.PreviewSHAs)

			if err == pgx.ErrNoRows {
				ownerUID = userUID
				_, err = tx.Exec(r.Context(), `
					INSERT INTO resources (resource_id, title, summary, description, restype, authors, owner_uid,
					                      tags, paid_type, icon_sha256, cover_sha256, preview_shas, moderation_state)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 'suspended')
				`, req.ResourceID, req.Title, req.Summary, req.Description, req.Restype, authorsJSON, ownerUID,
					tagsJSON, req.PaidType, req.IconSHA256, req.CoverSHA256, previewsJSON)
				if err != nil {
					return err
				}
			} else if err == nil {
				if ownerUID != userUID {
					return pgx.ErrTxClosed
				}
			} else {
				return err
			}

			err = tx.QueryRow(r.Context(), `
				INSERT INTO resource_revisions (resource_id, version, name, summary, changelog, downloads, state, creator_uid)
				VALUES ($1, $2, $3, $4, $5, $6, 'waiting_review', $7)
				RETURNING id
			`, req.ResourceID, req.Version, req.Title, req.Summary, req.Changelog, req.Downloads, userUID).Scan(&revID)
			if err != nil {
				return err
			}

			_, err = tx.Exec(r.Context(), `
				INSERT INTO review_cases (revision_id, state)
				VALUES ($1, 'pending')
				ON CONFLICT (revision_id) DO UPDATE SET state = 'pending', updated_at = now()
			`, revID)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "submit_failed", "提审失败，请检查包名归属或版本号是否重复")
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":      "waiting_review",
			"revision_id": revID,
		})
	}
}

// HandleCreatorGetSubmissions implements GET /api/v1/creator/submissions (Compatibility alias)
func HandleCreatorGetSubmissions(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || userUID <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT r.id, r.resource_id, r.version, r.name, r.state, r.review_note,
			       to_char(r.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       COALESCE(to_char(r.reviewed_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), '')
			FROM resource_revisions r
			WHERE r.creator_uid = $1
			ORDER BY r.created_at DESC
		`, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		type SubItem struct {
			ID         int64  `json:"id"`
			ResourceID string `json:"resource_id"`
			Version    string `json:"version"`
			Name       string `json:"name"`
			State      string `json:"state"`
			ReviewNote string `json:"review_note"`
			CreatedAt  string `json:"created_at"`
			ReviewedAt string `json:"reviewed_at,omitempty"`
		}

		var list []SubItem
		for rows.Next() {
			var it SubItem
			if err := rows.Scan(&it.ID, &it.ResourceID, &it.Version, &it.Name, &it.State, &it.ReviewNote, &it.CreatedAt, &it.ReviewedAt); err == nil {
				list = append(list, it)
			}
		}
		if list == nil {
			list = []SubItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": list})
	}
}
