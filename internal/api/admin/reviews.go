package admin

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/syndication"
)

type ReviewCaseItem struct {
	Kind        string          `json:"kind"`
	CaseID      int64           `json:"case_id"`
	RevisionID  int64           `json:"revision_id"`
	ResourceID  string          `json:"resource_id"`
	Title       string          `json:"title"`
	Version     string          `json:"version"`
	Changelog   string          `json:"changelog"`
	Downloads   json.RawMessage `json:"downloads"`
	State       string          `json:"state"`
	CreatorUID  int             `json:"creator_uid"`
	CreatorName string          `json:"creator_name"`
	ReviewerUID *int            `json:"reviewer_uid,omitempty"`
	ReviewNote  string          `json:"review_note"`
	CreatedAt   string          `json:"created_at"`
}

// HandleAdminGetReviews implements GET /admin/api/reviews
func HandleAdminGetReviews(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "管理员身份验证失败")
			return
		}

		state := strings.TrimSpace(r.URL.Query().Get("state"))
		if state == "" {
			state = "pending"
		}
		page := httputil.ParseInt(r.URL.Query().Get("page"), 1)
		size := httputil.ParseInt(r.URL.Query().Get("size"), 50)
		offset := (page - 1) * size

		query := `SELECT kind, case_id, revision_id, resource_id, title, version, changelog, downloads,
		                 state, creator_uid, creator_name, reviewer_uid, review_note, created_at
		          FROM (
					SELECT 'resource' AS kind, rc.id AS case_id, rr.id AS revision_id, rr.resource_id, rr.name AS title, rr.version,
					       rr.changelog, rr.downloads, rc.state, rr.creator_uid, COALESCE(u.username, ''),
					       rc.assigned_reviewer_uid, rc.note,
					       to_char(rc.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ') AS created_at
					FROM review_cases rc
					JOIN resource_revisions rr ON rc.revision_id = rr.id
					LEFT JOIN users u ON rr.creator_uid = u.bandbbs_uid
					UNION ALL
					SELECT 'collection' AS kind, 0::bigint AS case_id, cr.id AS revision_id, cr.collection_id AS resource_id, cr.name,
					       '' AS version, cr.summary, cr.items,
					       CASE WHEN cr.state = 'waiting_review' THEN 'pending' ELSE cr.state END,
					       c.owner_uid, COALESCE(u.username, ''),
					       cr.reviewer_uid, cr.review_note,
					       to_char(cr.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ') AS created_at
					FROM resource_collection_revisions cr
					JOIN resource_collections c ON c.id = cr.collection_id
					LEFT JOIN users u ON c.owner_uid = u.bandbbs_uid
				) review_items WHERE 1=1`
		countQuery := `SELECT count(*) FROM (
			SELECT rc.state FROM review_cases rc
			UNION ALL
			SELECT CASE WHEN cr.state = 'waiting_review' THEN 'pending' ELSE cr.state END FROM resource_collection_revisions cr
		) review_items WHERE 1=1`
		var args []any

		if state != "all" {
			query += ` AND review_items.state = $1`
			countQuery += ` AND review_items.state = $1`
			args = append(args, state)
		}

		var total int
		if err := s.Pool.QueryRow(r.Context(), countQuery, args...).Scan(&total); err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		idx1 := len(args) + 1
		idx2 := len(args) + 2
		query += fmt.Sprintf(` ORDER BY review_items.created_at DESC LIMIT $%d OFFSET $%d`, idx1, idx2)
		args = append(args, size, offset)

		rows, err := s.Pool.Query(r.Context(), query, args...)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var list []ReviewCaseItem
		for rows.Next() {
			var it ReviewCaseItem
			if err := rows.Scan(&it.Kind, &it.CaseID, &it.RevisionID, &it.ResourceID, &it.Title, &it.Version, &it.Changelog, &it.Downloads,
				&it.State, &it.CreatorUID, &it.CreatorName, &it.ReviewerUID, &it.ReviewNote, &it.CreatedAt); err == nil {
				list = append(list, it)
			}
		}
		if list == nil {
			list = []ReviewCaseItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"items": list,
			"total": total,
			"page":  page,
			"size":  size,
		})
	}
}

type AdminReviewDecisionRequest struct {
	Action string `json:"action"` // approve, reject, request_changes
	Reason string `json:"reason"`
}

type publicationSeed struct {
	Provider   string
	CategoryID int
	State      string
	Error      string
	Config     json.RawMessage
}

func buildPublicationSeeds(targets []string, raw, downloadsRaw json.RawMessage, configDir string) []publicationSeed {
	seeds, _ := buildPublicationSeedsWithSource(context.Background(), targets, raw, downloadsRaw, syndication.PublicationConfigSource{LocalDir: configDir})
	return seeds
}

func buildPublicationSeedsWithSource(ctx context.Context, targets []string, raw, downloadsRaw json.RawMessage, source syndication.PublicationConfigSource) ([]publicationSeed, error) {
	var cfg syndication.SubmissionConfig
	_ = json.Unmarshal(raw, &cfg)
	var downloads map[string]syndication.DownloadEntry
	_ = json.Unmarshal(downloadsRaw, &downloads)
	var devices map[string]syndication.DeviceSpec
	var mapping syndication.PublishConfig
	needsBandBBS := false
	for _, provider := range targets {
		if provider == "bandbbs" {
			needsBandBBS = true
			break
		}
	}
	if needsBandBBS {
		var err error
		devices, mapping, err = source.Load(ctx)
		if err != nil {
			return nil, err
		}
	}
	seeds := make([]publicationSeed, 0)
	for _, provider := range targets {
		if provider == "oronbox" {
			continue
		}
		var plans []syndication.PublicationPlan
		plans, err := syndication.PlanPublications([]string{provider}, downloads, devices, mapping)
		if err != nil {
			seeds = append(seeds, publicationSeed{Provider: provider, State: "failed", Error: "publication category mapping unavailable", Config: json.RawMessage(`{}`)})
			continue
		}
		for _, plan := range plans {
			if provider == "bandbbs" && cfg.BandBBS.Purchase != nil {
				plan.Config["purchase"] = cfg.BandBBS.Purchase
			}
			encoded, _ := json.Marshal(plan.Config)
			seeds = append(seeds, publicationSeed{Provider: provider, CategoryID: plan.CategoryID, Config: encoded})
		}
	}
	return seeds, nil
}

// HandleAdminDecideReview implements POST /admin/api/reviews/revisions/{id}
func HandleAdminDecideReview(s *store.Store, storageDirs ...string) http.HandlerFunc {
	storageDir := "/var/lib/oronbox/blobs"
	configDir := ""
	if len(storageDirs) > 1 {
		configDir = storageDirs[1]
	}
	if len(storageDirs) > 0 && strings.TrimSpace(storageDirs[0]) != "" {
		storageDir = storageDirs[0]
	}
	return handleAdminDecideReview(s, storageDir, syndication.PublicationConfigSource{LocalDir: configDir})
}

// HandleAdminDecideReviewWithPublicationSource uses the central Gitea
// publication mappings configured by the server.
func HandleAdminDecideReviewWithPublicationSource(s *store.Store, storageDir string, source syndication.PublicationConfigSource) http.HandlerFunc {
	if strings.TrimSpace(storageDir) == "" {
		storageDir = "/var/lib/oronbox/blobs"
	}
	return handleAdminDecideReview(s, storageDir, source)
}

func handleAdminDecideReview(s *store.Store, storageDir string, source syndication.PublicationConfigSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "管理员身份验证失败")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 5 {
			httputil.Error(w, http.StatusBadRequest, "invalid_path", "路径错误")
			return
		}
		revID := httputil.ParseInt(parts[4], 0)
		if revID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "无效的版本ID")
			return
		}

		var req AdminReviewDecisionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "请求体格式错误")
			return
		}
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Find revision & resource
			var resID string
			var curRevState string
			var creatorUID int
			var downloadsRaw json.RawMessage
			var configRaw json.RawMessage
			err := tx.QueryRow(r.Context(), `
					SELECT resource_id, state, creator_uid, downloads, config
				FROM resource_revisions WHERE id = $1 FOR UPDATE
				`, revID).Scan(&resID, &curRevState, &creatorUID, &downloadsRaw, &configRaw)
			if err != nil {
				return err
			}
			if req.Action != "" && curRevState != "waiting_review" {
				return fmt.Errorf("revision_not_waiting_review")
			}

			// Find case ID
			var caseID int64
			_ = tx.QueryRow(r.Context(), `SELECT id FROM review_cases WHERE revision_id = $1`, revID).Scan(&caseID)

			switch req.Action {
			case "approve":
				// 1. Mark previous approved revisions as superseded
				_, _ = tx.Exec(r.Context(), `
					UPDATE resource_revisions SET state = 'superseded' 
					WHERE resource_id = $1 AND state = 'approved'
				`, resID)

				// 2. Mark this revision approved
				_, err = tx.Exec(r.Context(), `
					UPDATE resource_revisions 
					SET state = 'approved', reviewer_uid = $1, review_note = $2, reviewed_at = now(), updated_at = now()
						WHERE id = $3 AND state = 'waiting_review'
				`, adminUID, req.Reason, revID)
				if err != nil {
					return err
				}
				// 3. Extract devices from downloads JSONB map to update resource.devices
				var dlMap map[string]any
				_ = json.Unmarshal(downloadsRaw, &dlMap)
				devList := make([]string, 0, len(dlMap))
				for d := range dlMap {
					devList = append(devList, d)
				}
				devJSON, _ := json.Marshal(devList)

				// 4. Update parent resource: current_revision_id, visible, published_at, last_released_at
				_, err = tx.Exec(r.Context(), `
					UPDATE resources 
					SET current_revision_id = $1, moderation_state = 'visible', devices = $2,
					    published_at = COALESCE(published_at, now()), last_released_at = now(), updated_at = now()
					WHERE resource_id = $3
				`, revID, devJSON, resID)
				if err != nil {
					return err
				}

				var cfg struct {
					Targets       []string `json:"targets"`
					DraftMetadata struct {
						IconSHA256  string   `json:"icon_sha256"`
						CoverSHA256 string   `json:"cover_sha256"`
						PreviewSHAs []string `json:"preview_shas"`
						Tags        []string `json:"tags"`
						Description string   `json:"description"`
					} `json:"draft_metadata"`
				}
				_ = json.Unmarshal(configRaw, &cfg)
				if strings.TrimSpace(cfg.DraftMetadata.IconSHA256) == "" {
					return fmt.Errorf("icon_required")
				}
				if len(cfg.DraftMetadata.IconSHA256) != 64 {
					return fmt.Errorf("invalid_metadata_blob")
				}
				iconPath := filepath.Join(storageDir, cfg.DraftMetadata.IconSHA256[:2], cfg.DraftMetadata.IconSHA256[2:4], cfg.DraftMetadata.IconSHA256)
				iconData, iconErr := os.ReadFile(iconPath)
				if iconErr != nil {
					iconData, iconErr = os.ReadFile(filepath.Join(storageDir, cfg.DraftMetadata.IconSHA256))
				}
				if iconErr != nil || syndication.ValidateIconWebP(iconData) != nil {
					return fmt.Errorf("icon_must_be_512_webp")
				}
				for _, sha := range append([]string{cfg.DraftMetadata.IconSHA256, cfg.DraftMetadata.CoverSHA256}, cfg.DraftMetadata.PreviewSHAs...) {
					if sha == "" {
						continue
					}
					if len(sha) != 64 {
						return fmt.Errorf("invalid_metadata_blob")
					}
					if _, decodeErr := hex.DecodeString(sha); decodeErr != nil {
						return fmt.Errorf("invalid_metadata_blob")
					}
					var exists bool
					if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM blobs WHERE sha256 = $1)`, sha).Scan(&exists); err != nil || !exists {
						return fmt.Errorf("metadata_blob_not_found")
					}
					if sha == cfg.DraftMetadata.IconSHA256 {
						var mediaType string
						if err := tx.QueryRow(r.Context(), `SELECT media_type FROM blobs WHERE sha256 = $1`, sha).Scan(&mediaType); err != nil || !strings.EqualFold(strings.TrimSpace(strings.Split(mediaType, ";")[0]), "image/webp") {
							return fmt.Errorf("icon_must_be_webp")
						}
					}
				}
				for _, tag := range cfg.DraftMetadata.Tags {
					var enabled bool
					if err := tx.QueryRow(r.Context(), `SELECT enabled FROM tags WHERE name = $1`, tag).Scan(&enabled); err != nil || !enabled {
						return fmt.Errorf("invalid_tag")
					}
				}
				tagsJSON, _ := json.Marshal(cfg.DraftMetadata.Tags)
				if _, err = tx.Exec(r.Context(), `
					UPDATE tags t SET usage_count = GREATEST(0, t.usage_count - 1)
					WHERE t.name IN (
						SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(r.tags) = 'array' THEN r.tags ELSE '[]'::jsonb END)
						FROM resources r WHERE r.resource_id = $1
					)
				`, resID); err != nil {
					return err
				}
				if len(cfg.DraftMetadata.Tags) > 0 {
					if _, err = tx.Exec(r.Context(), `
						UPDATE tags t SET usage_count = t.usage_count + 1
						WHERE t.name IN (SELECT jsonb_array_elements_text($1::jsonb))
					`, tagsJSON); err != nil {
						return err
					}
				}
				previewJSON, _ := json.Marshal(cfg.DraftMetadata.PreviewSHAs)
				_, err = tx.Exec(r.Context(), `
						UPDATE resources SET
							icon_sha256 = COALESCE(NULLIF($1, ''), icon_sha256),
							cover_sha256 = COALESCE(NULLIF($2, ''), cover_sha256),
					preview_shas = CASE WHEN jsonb_typeof($3) = 'array' THEN $3 ELSE preview_shas END,
						tags = CASE WHEN jsonb_typeof($4) = 'array' THEN $4 ELSE tags END,
							description = COALESCE(NULLIF($5, ''), description)
						WHERE resource_id = $6
					`, cfg.DraftMetadata.IconSHA256, cfg.DraftMetadata.CoverSHA256, previewJSON, tagsJSON, cfg.DraftMetadata.Description, resID)
				if err != nil {
					return err
				}
				seeds, seedErr := buildPublicationSeedsWithSource(r.Context(), cfg.Targets, configRaw, downloadsRaw, source)
				if seedErr != nil {
					return seedErr
				}
				for _, seed := range seeds {
					state := seed.State
					if state == "" {
						state = "pending"
					}
					if _, err = tx.Exec(r.Context(), `
						INSERT INTO publications (revision_id, provider, category_id, state, config, error_message, attempts, next_attempt_at)
						VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $4 = 'failed' THEN 5 ELSE 0 END, CASE WHEN $4 = 'failed' THEN NULL ELSE now() END)
						ON CONFLICT (revision_id, provider, category_id) DO UPDATE
						SET state = EXCLUDED.state, config = EXCLUDED.config, error_message = EXCLUDED.error_message,
						    attempts = EXCLUDED.attempts, next_attempt_at = EXCLUDED.next_attempt_at, updated_at = now()
					`, revID, seed.Provider, seed.CategoryID, state, seed.Config, seed.Error); err != nil {
						return err
					}
				}

				// 5. Update review_case state
				_, _ = tx.Exec(r.Context(), `
					UPDATE review_cases 
					SET state = 'approved', assigned_reviewer_uid = $1, note = $2, updated_at = now()
					WHERE revision_id = $3
				`, adminUID, req.Reason, revID)

				if caseID > 0 {
					_, _ = tx.Exec(r.Context(), `
						INSERT INTO review_case_events (case_id, actor_uid, event, note)
						VALUES ($1, $2, 'approved', $3)
					`, caseID, adminUID, req.Reason)
				}

				// 6. Send notification message to creator
				_, _ = tx.Exec(r.Context(), `
					INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data)
					VALUES ($1, 'system', 'review.approved', '资源版本审核已通过', '你的资源版本审核已通过并正式上线发布', $2, jsonb_build_object('status','approved','revision_id',$3,'review_id',$3))
				`, creatorUID, resID, revID)

			case "reject":
				if _, err := tx.Exec(r.Context(), `UPDATE publications SET state = 'cancelled', next_attempt_at = NULL, updated_at = now() WHERE revision_id = $1`, revID); err != nil {
					return err
				}
				_, err = tx.Exec(r.Context(), `
					UPDATE resource_revisions 
					SET state = 'rejected', reviewer_uid = $1, review_note = $2, reviewed_at = now(), updated_at = now()
					WHERE id = $3
				`, adminUID, req.Reason, revID)
				if err != nil {
					return err
				}

				_, _ = tx.Exec(r.Context(), `
					UPDATE review_cases 
					SET state = 'rejected', assigned_reviewer_uid = $1, note = $2, updated_at = now()
					WHERE revision_id = $3
				`, adminUID, req.Reason, revID)

				if caseID > 0 {
					_, _ = tx.Exec(r.Context(), `
						INSERT INTO review_case_events (case_id, actor_uid, event, note)
						VALUES ($1, $2, 'rejected', $3)
					`, caseID, adminUID, req.Reason)
				}

				_, _ = tx.Exec(r.Context(), `
					INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data)
					VALUES ($1, 'system', 'review.rejected', '资源版本审核未通过', $2, $3, jsonb_build_object('status','rejected','revision_id',$4,'review_id',$4,'reason',$2))
				`, creatorUID, "驳回原因："+req.Reason, resID, revID)

			case "request_changes":
				_, err = tx.Exec(r.Context(), `
					UPDATE resource_revisions 
					SET state = 'changes_requested', reviewer_uid = $1, review_note = $2, reviewed_at = now(), updated_at = now()
					WHERE id = $3
				`, adminUID, req.Reason, revID)
				if err != nil {
					return err
				}

				_, _ = tx.Exec(r.Context(), `
					UPDATE review_cases 
					SET state = 'pending', assigned_reviewer_uid = $1, note = $2, updated_at = now()
					WHERE revision_id = $3
				`, adminUID, req.Reason, revID)

				if caseID > 0 {
					_, _ = tx.Exec(r.Context(), `
						INSERT INTO review_case_events (case_id, actor_uid, event, note)
						VALUES ($1, $2, 'request_changes', $3)
					`, caseID, adminUID, req.Reason)
				}

				_, _ = tx.Exec(r.Context(), `
					INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data)
					VALUES ($1, 'system', 'review.changes_requested', '资源审核需要修改', $2, $3, jsonb_build_object('status','changes_requested','revision_id',$4,'review_id',$4,'reason',$2))
				`, creatorUID, "修改建议："+req.Reason, resID, revID)

			default:
				return fmt.Errorf("unsupported action: %s", req.Action)
			}

			// Audit log
			targetData, _ := json.Marshal(map[string]any{"revision_id": revID, "action": req.Action, "reason": req.Reason})
			_, _ = tx.Exec(r.Context(), `
				INSERT INTO audit_logs (actor_uid, action, result, ip, target_data)
				VALUES ($1, 'review_decision', $2, $3, $4)
			`, adminUID, req.Action, r.RemoteAddr, targetData)

			return nil
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "review_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"status": "success", "action": req.Action})
	}
}

// HandleAdminGetReviewDetail implements GET /admin/api/reviews/revisions/:id (Spec §14.2)
func HandleAdminGetReviewDetail(s *store.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未授权")
			return
		}

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		var revID int64
		for idx, p := range parts {
			if p == "revisions" && idx+1 < len(parts) {
				revID = int64(httputil.ParseInt(parts[idx+1], 0))
				break
			}
		}
		if revID <= 0 {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "无效的版本ID")
			return
		}

		var (
			resID       string
			version     string
			name        string
			summary     string
			changelog   string
			downloads   json.RawMessage
			state       string
			creatorUID  int
			reviewNote  string
			createdAt   string
			reviewedAt  string
			title       string
			desc        string
			iconSHA     string
			coverSHA    string
			previewsRaw json.RawMessage
			restype     string
		)

		err = s.Pool.QueryRow(r.Context(), `
			SELECT rr.resource_id, rr.version, rr.name, rr.summary, rr.changelog, rr.downloads, rr.state,
			       rr.creator_uid, rr.review_note, to_char(rr.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'),
			       COALESCE(to_char(rr.reviewed_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), ''),
			       r.title, r.description, r.icon_sha256, r.cover_sha256, r.preview_shas, r.restype
			FROM resource_revisions rr
			JOIN resources r ON rr.resource_id = r.resource_id
			WHERE rr.id = $1
		`, revID).Scan(&resID, &version, &name, &summary, &changelog, &downloads, &state,
			&creatorUID, &reviewNote, &createdAt, &reviewedAt,
			&title, &desc, &iconSHA, &coverSHA, &previewsRaw, &restype)

		if err == pgx.ErrNoRows {
			httputil.Error(w, http.StatusNotFound, "revision_not_found", "提审版本不存在")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		var dlMap map[string]struct {
			BlobSHA256   string `json:"blob_sha256"`
			OriginalName string `json:"original_name"`
			SizeBytes    int64  `json:"size_bytes"`
		}
		_ = json.Unmarshal(downloads, &dlMap)

		downloadsWithURL := make(map[string]any)
		for dev, it := range dlMap {
			downloadsWithURL[dev] = map[string]any{
				"blob_sha256":   it.BlobSHA256,
				"original_name": it.OriginalName,
				"size_bytes":    it.SizeBytes,
				"download_url":  fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, it.BlobSHA256),
			}
		}

		var previews []string
		_ = json.Unmarshal(previewsRaw, &previews)
		previewURLs := make([]string, 0, len(previews))
		for _, sha := range previews {
			if sha != "" {
				previewURLs = append(previewURLs, fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, sha))
			}
		}
		type publicationView struct {
			ID          int64  `json:"id"`
			Provider    string `json:"provider"`
			CategoryID  int    `json:"category_id"`
			State       string `json:"state"`
			Error       string `json:"error_message,omitempty"`
			ExternalID  string `json:"external_id,omitempty"`
			ExternalURL string `json:"external_url,omitempty"`
		}
		pubRows, _ := s.Pool.Query(r.Context(), `SELECT id, provider, category_id, state, COALESCE(error_message, ''), COALESCE(external_id, ''), COALESCE(external_url, '') FROM publications WHERE revision_id = $1 ORDER BY provider, category_id`, revID)
		publications := make([]publicationView, 0)
		states := make([]string, 0)
		if pubRows != nil {
			defer pubRows.Close()
			for pubRows.Next() {
				var p publicationView
				if pubRows.Scan(&p.ID, &p.Provider, &p.CategoryID, &p.State, &p.Error, &p.ExternalID, &p.ExternalURL) == nil {
					publications = append(publications, p)
					states = append(states, p.State)
				}
			}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"revision_id":       revID,
			"resource_id":       resID,
			"version":           version,
			"name":              name,
			"title":             title,
			"summary":           summary,
			"description":       desc,
			"changelog":         changelog,
			"restype":           restype,
			"state":             state,
			"creator_uid":       creatorUID,
			"review_note":       reviewNote,
			"created_at":        createdAt,
			"reviewed_at":       reviewedAt,
			"icon_url":          fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, iconSHA),
			"cover_url":         fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, coverSHA),
			"preview_urls":      previewURLs,
			"downloads":         downloadsWithURL,
			"publications":      publications,
			"publication_state": syndication.Rollup(states),
		})
	}
}

// HandleAdminDecideCollectionReview implements POST /admin/api/reviews/collections/:id (Spec §14.2)
func HandleAdminDecideCollectionReview(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminUID, _, err := httputil.AuthenticateAdmin(r.Context(), s, r)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "管理员身份验证失败")
			return
		}

		collID := r.PathValue("id")
		requestedRevisionID := int64(0)
		if parsed := httputil.ParseInt(collID, 0); parsed > 0 {
			requestedRevisionID = int64(parsed)
			collID = ""
		}
		if collID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "collections" && idx+1 < len(parts) {
					candidate := parts[idx+1]
					if parsed := httputil.ParseInt(candidate, 0); parsed > 0 {
						requestedRevisionID = int64(parsed)
					} else {
						collID = candidate
					}
					break
				}
			}
		}

		var req struct {
			Action string `json:"action"` // approve, request_changes, reject
			Note   string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "请求体格式错误")
			return
		}

		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Find waiting_review revision
			var revID int64
			var ownerUID int
			var err error
			if requestedRevisionID > 0 {
				err = tx.QueryRow(r.Context(), `SELECT id, collection_id FROM resource_collection_revisions WHERE id = $1 AND state = 'waiting_review' FOR UPDATE`, requestedRevisionID).Scan(&revID, &collID)
			} else {
				err = tx.QueryRow(r.Context(), `SELECT id FROM resource_collection_revisions WHERE collection_id = $1 AND state = 'waiting_review' ORDER BY revision_no DESC LIMIT 1 FOR UPDATE`, collID).Scan(&revID)
			}
			if err != nil {
				return fmt.Errorf("no_waiting_review_revision")
			}
			if err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resource_collections WHERE id = $1`, collID).Scan(&ownerUID); err != nil {
				return err
			}

			switch req.Action {
			case "approve":
				// Only publish collections whose every referenced resource is currently
				// visible and has an approved revision.
				var itemRefs []struct {
					ResourceID string `json:"resource_id"`
				}
				var itemsRaw json.RawMessage
				if err := tx.QueryRow(r.Context(), `SELECT items FROM resource_collection_revisions WHERE id = $1`, revID).Scan(&itemsRaw); err != nil {
					return err
				}
				if err := json.Unmarshal(itemsRaw, &itemRefs); err != nil || len(itemRefs) == 0 {
					return fmt.Errorf("invalid_collection_items")
				}
				coinCount := 0
				for _, item := range itemRefs {
					var value int
					if err := tx.QueryRow(r.Context(), `SELECT coin_count FROM resources WHERE resource_id = $1 AND moderation_state = 'visible' AND current_revision_id IS NOT NULL`, item.ResourceID).Scan(&value); err != nil {
						return fmt.Errorf("collection_resource_not_published")
					}
					coinCount += value
				}
				_, _ = tx.Exec(r.Context(), `
					UPDATE resource_collection_revisions SET state = 'superseded' 
					WHERE collection_id = $1 AND state = 'approved' AND id != $2
				`, collID, revID)

				_, err = tx.Exec(r.Context(), `
					UPDATE resource_collection_revisions 
					SET state = 'approved', reviewer_uid = $1, review_note = $2, updated_at = now()
					WHERE id = $3
				`, adminUID, req.Note, revID)
				if err != nil {
					return err
				}

				_, err = tx.Exec(r.Context(), `
					UPDATE resource_collections 
					SET current_revision_id = $1, coin_count = $2, moderation_state = 'visible', published_at = now(), updated_at = now()
					WHERE id = $3
				`, revID, coinCount, collID)
				if err != nil {
					return err
				}
				_, _ = tx.Exec(r.Context(), `INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data) VALUES ($1, 'system', 'collection.approved', $2, $3, $4, jsonb_build_object('kind','collection','collection_id',$4,'revision_id',$5))`, ownerUID, "合集审核已通过", "你的合集已通过审核并发布", collID, revID)
				_, _ = tx.Exec(r.Context(), `INSERT INTO audit_logs (actor_uid, action, result, ip, target_data) VALUES ($1, 'admin_review_collection', 'success', $2, $3)`, adminUID, r.RemoteAddr, fmt.Sprintf(`{"collection_id":%q,"action":%q}`, collID, req.Action))
				return nil

			case "request_changes":
				_, err = tx.Exec(r.Context(), `
					UPDATE resource_collection_revisions 
					SET state = 'changes_requested', reviewer_uid = $1, review_note = $2, updated_at = now()
					WHERE id = $3
				`, adminUID, req.Note, revID)
				if err != nil {
					return err
				}
				_, _ = tx.Exec(r.Context(), `INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data) VALUES ($1, 'system', 'collection.changes_requested', $2, $3, $4, jsonb_build_object('kind','collection','collection_id',$4,'revision_id',$5,'note',$3))`, ownerUID, "合集需要修改", req.Note, collID, revID)
				return nil

			case "reject":
				_, err = tx.Exec(r.Context(), `
					UPDATE resource_collection_revisions 
					SET state = 'rejected', reviewer_uid = $1, review_note = $2, updated_at = now()
					WHERE id = $3
				`, adminUID, req.Note, revID)
				if err != nil {
					return err
				}
				_, _ = tx.Exec(r.Context(), `INSERT INTO user_messages (user_uid, kind, event, title, body, ref, data) VALUES ($1, 'system', 'collection.rejected', $2, $3, $4, jsonb_build_object('kind','collection','collection_id',$4,'revision_id',$5,'note',$3))`, ownerUID, "合集审核未通过", req.Note, collID, revID)
				return nil

			default:
				return fmt.Errorf("unsupported action: %s", req.Action)
			}
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "review_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"status": "success", "action": req.Action})
	}
}
