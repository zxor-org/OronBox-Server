package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type ReviewCaseItem struct {
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

		query := `SELECT rc.id, rc.revision_id, rr.resource_id, rr.name, rr.version, rr.changelog, rr.downloads,
		                 rc.state, rr.creator_uid, COALESCE(u.username, ''), rc.assigned_reviewer_uid, rc.note,
		                 to_char(rc.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ')
		          FROM review_cases rc
		          JOIN resource_revisions rr ON rc.revision_id = rr.id
		          LEFT JOIN users u ON rr.creator_uid = u.bandbbs_uid
		          WHERE 1=1`
		countQuery := `SELECT count(*) FROM review_cases rc WHERE 1=1`
		var args []any

		if state != "all" {
			query += ` AND rc.state = $1`
			countQuery += ` AND rc.state = $1`
			args = append(args, state)
		}

		var total int
		_ = s.Pool.QueryRow(r.Context(), countQuery, args...).Scan(&total)

		idx1 := len(args) + 1
		idx2 := len(args) + 2
		query += fmt.Sprintf(` ORDER BY rc.created_at DESC LIMIT $%d OFFSET $%d`, idx1, idx2)
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
			if err := rows.Scan(&it.CaseID, &it.RevisionID, &it.ResourceID, &it.Title, &it.Version, &it.Changelog, &it.Downloads,
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

// HandleAdminDecideReview implements POST /admin/api/reviews/revisions/{id}
func HandleAdminDecideReview(s *store.Store) http.HandlerFunc {
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
			err := tx.QueryRow(r.Context(), `
				SELECT resource_id, state, creator_uid, downloads 
				FROM resource_revisions WHERE id = $1 FOR UPDATE
			`, revID).Scan(&resID, &curRevState, &creatorUID, &downloadsRaw)
			if err != nil {
				return err
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
					WHERE id = $3
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
					VALUES ($1, 'system', 'review.approved', '资源版本审核已通过', '你的资源版本审核已通过并正式上线发布', $2, '{"status":"approved"}'::jsonb)
				`, creatorUID, resID)

			case "reject":
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
					VALUES ($1, 'system', 'review.rejected', '资源版本审核未通过', $2, $3, '{"status":"rejected"}'::jsonb)
				`, creatorUID, "驳回原因："+req.Reason, resID)

			case "request_changes":
				// Bug 3.3: revision.state='changes_requested', review_cases.state remains 'pending'
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
					VALUES ($1, 'system', 'review.changes_requested', '资源审核需要修改', $2, $3, '{"status":"changes_requested"}'::jsonb)
				`, creatorUID, "修改建议："+req.Reason, resID)

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

		httputil.JSON(w, http.StatusOK, map[string]any{
			"revision_id":  revID,
			"resource_id":  resID,
			"version":      version,
			"name":         name,
			"title":        title,
			"summary":      summary,
			"description":  desc,
			"changelog":    changelog,
			"restype":      restype,
			"state":        state,
			"creator_uid":  creatorUID,
			"review_note":  reviewNote,
			"created_at":   createdAt,
			"reviewed_at":  reviewedAt,
			"icon_url":     fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, iconSHA),
			"cover_url":    fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, coverSHA),
			"preview_urls": previewURLs,
			"downloads":    downloadsWithURL,
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
		if collID == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "collections" && idx+1 < len(parts) {
					collID = parts[idx+1]
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
			err := tx.QueryRow(r.Context(), `
				SELECT id FROM resource_collection_revisions 
				WHERE collection_id = $1 AND state = 'waiting_review'
				ORDER BY revision_no DESC LIMIT 1 FOR UPDATE
			`, collID).Scan(&revID)
			if err != nil {
				return fmt.Errorf("no_waiting_review_revision")
			}

			// Bug 2.3: resource_collection_revisions has updated_at, NOT reviewed_at
			switch req.Action {
			case "approve":
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
					SET current_revision_id = $1, moderation_state = 'visible', published_at = now(), updated_at = now()
					WHERE id = $2
				`, revID, collID)
				return err

			case "request_changes":
				_, err = tx.Exec(r.Context(), `
					UPDATE resource_collection_revisions 
					SET state = 'changes_requested', reviewer_uid = $1, review_note = $2, updated_at = now()
					WHERE id = $3
				`, adminUID, req.Note, revID)
				return err

			case "reject":
				_, err = tx.Exec(r.Context(), `
					UPDATE resource_collection_revisions 
					SET state = 'rejected', reviewer_uid = $1, review_note = $2, updated_at = now()
					WHERE id = $3
				`, adminUID, req.Note, revID)
				return err

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
