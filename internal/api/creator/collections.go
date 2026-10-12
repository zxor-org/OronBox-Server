package creator

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type CreatorCollItem struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	CoinCount       int    `json:"coin_count"`
	ModerationState string `json:"moderation_state"`
	CreatedAt       string `json:"created_at"`
	LatestName      string `json:"latest_name"`
	LatestSummary   string `json:"latest_summary"`
	LatestState     string `json:"latest_state"`
}

// HandleCreatorGetCollections implements GET /api/v1/creator/collections (Spec §12.5)
func HandleCreatorGetCollections(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		rows, err := s.Pool.Query(r.Context(), `
			SELECT c.id, c.kind, c.coin_count, c.moderation_state,
			       COALESCE(to_char(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SSZ'), ''),
			       COALESCE(cr.name, ''), COALESCE(cr.summary, ''), COALESCE(cr.state, 'draft')
			FROM resource_collections c
			LEFT JOIN resource_collection_revisions cr ON cr.id = (
				SELECT id FROM resource_collection_revisions WHERE collection_id = c.id ORDER BY revision_no DESC LIMIT 1
			)
			WHERE c.owner_uid = $1
			ORDER BY c.updated_at DESC
		`, userUID)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}
		defer rows.Close()

		var items []CreatorCollItem
		for rows.Next() {
			var it CreatorCollItem
			if err := rows.Scan(&it.ID, &it.Kind, &it.CoinCount, &it.ModerationState, &it.CreatedAt, &it.LatestName, &it.LatestSummary, &it.LatestState); err == nil {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []CreatorCollItem{}
		}

		httputil.JSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// HandleCreatorGetCollectionDetail returns the latest editable/published
// revision and expanded item references for the collection editor.
func HandleCreatorGetCollectionDetail(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, err := httputil.AuthenticateUser(r.Context(), s, httputil.ExtractBearerToken(r))
		if err != nil {
			httputil.Error(w, 401, "unauthorized", "未登录或Token失效")
			return
		}
		id := collectionID(r)
		var owner int
		var kind, moderation string
		var currentID *int64
		if err := s.Pool.QueryRow(r.Context(), `SELECT owner_uid,kind,moderation_state,current_revision_id FROM resource_collections WHERE id=$1`, id).Scan(&owner, &kind, &moderation, &currentID); err == pgx.ErrNoRows {
			httputil.Error(w, 404, "collection_not_found", "合集不存在")
			return
		} else if err != nil {
			httputil.Error(w, 500, "db_error", "读取合集失败")
			return
		}
		if owner != uid {
			httputil.Error(w, 403, "forbidden", "无权访问该合集")
			return
		}
		revisions := make([]map[string]any, 0)
		rows, err := s.Pool.Query(r.Context(), `SELECT id,revision_no,name,summary,description,icon_sha256,cover_sha256,representative_resource_id,items,state,reviewer_uid,review_note,created_at,updated_at FROM resource_collection_revisions WHERE collection_id=$1 ORDER BY revision_no DESC`, id)
		if err != nil {
			httputil.Error(w, 500, "db_error", "读取合集版本失败")
			return
		}
		defer rows.Close()
		var current, pending map[string]any
		for rows.Next() {
			var rid int64
			var number int
			var name, summary, description, icon, cover, representative, state, note, createdAt, updatedAt string
			var items json.RawMessage
			var reviewer *int
			if rows.Scan(&rid, &number, &name, &summary, &description, &icon, &cover, &representative, &items, &state, &reviewer, &note, &createdAt, &updatedAt) != nil {
				continue
			}
			entry := map[string]any{"id": rid, "revision_id": rid, "number": number, "revision_no": number, "name": name, "summary": summary, "description": description, "icon_sha256": icon, "cover_sha256": cover, "representative_resource_id": representative, "items": items, "state": state, "reviewer_uid": reviewer, "review_note": note, "created_at": createdAt, "updated_at": updatedAt}
			revisions = append(revisions, entry)
			if currentID != nil && *currentID == rid {
				current = entry
			}
			if pending == nil && (state == "draft" || state == "waiting_review" || state == "changes_requested") {
				pending = entry
			}
		}
		items := make([]map[string]any, 0)
		baseline := current
		if pending != nil {
			baseline = pending
		}
		if baseline != nil {
			if raw, ok := baseline["items"].(json.RawMessage); ok {
				var refs []struct {
					ResourceID string `json:"resource_id"`
					SortOrder  int    `json:"sort_order"`
				}
				if json.Unmarshal(raw, &refs) == nil {
					for _, ref := range refs {
						var resourceID, title, summary, restype, iconSHA, version string
						var downloads int
						if s.Pool.QueryRow(r.Context(), `SELECT r.resource_id,r.title,r.summary,r.restype,r.icon_sha256,COALESCE(rr.version,''),r.download_count FROM resources r LEFT JOIN resource_revisions rr ON rr.id=r.current_revision_id WHERE r.resource_id=$1`, ref.ResourceID).Scan(&resourceID, &title, &summary, &restype, &iconSHA, &version, &downloads) == nil {
							items = append(items, map[string]any{"resource_id": resourceID, "sort_order": ref.SortOrder, "title": title, "summary": summary, "restype": restype, "icon_sha256": iconSHA, "version": version, "download_count": downloads})
						}
					}
				}
			}
		}
		httputil.JSON(w, 200, map[string]any{"id": id, "collection_id": id, "kind": kind, "moderation_state": moderation, "current_revision": current, "pending_revision": pending, "revisions": revisions, "items": items})
	}
}

// HandleCreatorSaveCollection implements POST /api/v1/creator/collections (Spec §12.6)
func HandleCreatorSaveCollection(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		var req struct {
			ID                       string          `json:"id"`
			Name                     string          `json:"name"`
			Summary                  string          `json:"summary"`
			Description              string          `json:"description"`
			Kind                     string          `json:"kind"`
			IconSHA256               string          `json:"icon_sha256"`
			CoverSHA256              string          `json:"cover_sha256"`
			RepresentativeResourceID string          `json:"representative_resource_id"`
			Items                    json.RawMessage `json:"items"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "参数格式错误")
			return
		}
		req.ID = strings.TrimSpace(req.ID)
		if req.ID == "" || req.Name == "" {
			httputil.Error(w, http.StatusBadRequest, "missing_fields", "缺少合集ID或名称")
			return
		}
		if req.Kind == "" {
			req.Kind = "quick_app"
		}
		if req.Kind != "quick_app" && req.Kind != "watchface" && req.Kind != "plugin" {
			httputil.Error(w, http.StatusBadRequest, "invalid_kind", "不支持的合集类型")
			return
		}
		if len([]rune(req.Name)) > 128 || len([]rune(req.Summary)) > 500 || len([]rune(req.Description)) > 20000 {
			httputil.Error(w, http.StatusBadRequest, "field_too_long", "合集字段长度超限")
			return
		}
		if req.Items == nil {
			req.Items = json.RawMessage("[]")
		}
		var itemRefs []struct {
			ResourceID string `json:"resource_id"`
			SortOrder  int    `json:"sort_order"`
		}
		if err := json.Unmarshal(req.Items, &itemRefs); err != nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_items", "合集条目格式错误")
			return
		}
		seen := make(map[string]struct{}, len(itemRefs))
		seenOrder := make(map[int]struct{}, len(itemRefs))
		allZeroOrder := true
		for _, item := range itemRefs {
			if item.SortOrder != 0 {
				allZeroOrder = false
				break
			}
		}
		for i := range itemRefs {
			itemRefs[i].ResourceID = strings.TrimSpace(itemRefs[i].ResourceID)
			if allZeroOrder {
				itemRefs[i].SortOrder = i
			}
			if itemRefs[i].ResourceID == "" || itemRefs[i].SortOrder < 0 {
				httputil.Error(w, http.StatusBadRequest, "invalid_items", "合集条目无效")
				return
			}
			if _, ok := seen[itemRefs[i].ResourceID]; ok {
				httputil.Error(w, http.StatusBadRequest, "duplicate_item", "合集不能包含重复资源")
				return
			}
			if _, ok := seenOrder[itemRefs[i].SortOrder]; ok {
				httputil.Error(w, http.StatusBadRequest, "duplicate_sort_order", "合集排序值不能重复")
				return
			}
			seen[itemRefs[i].ResourceID] = struct{}{}
			seenOrder[itemRefs[i].SortOrder] = struct{}{}
		}
		if req.RepresentativeResourceID != "" {
			if _, ok := seen[req.RepresentativeResourceID]; !ok {
				httputil.Error(w, http.StatusBadRequest, "invalid_representative", "代表资源必须属于合集")
				return
			}
		}
		if normalizedItems, marshalErr := json.Marshal(itemRefs); marshalErr == nil {
			req.Items = normalizedItems
		}

		var revID int64
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Ensure collection exists and owned by user
			var ownerUID int
			qErr := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resource_collections WHERE id = $1`, req.ID).Scan(&ownerUID)
			if qErr == pgx.ErrNoRows {
				_, err = tx.Exec(r.Context(), `
					INSERT INTO resource_collections (id, owner_uid, kind, moderation_state)
					VALUES ($1, $2, $3, 'suspended')
				`, req.ID, userUID, req.Kind)
				if err != nil {
					return err
				}
			} else if qErr != nil {
				return qErr
			} else if ownerUID != userUID {
				return fmt.Errorf("forbidden")
			}
			for _, item := range itemRefs {
				if strings.TrimSpace(item.ResourceID) == "" {
					return fmt.Errorf("invalid_item")
				}
				var exists bool
				if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM resources WHERE resource_id = $1 AND moderation_state = 'visible' AND current_revision_id IS NOT NULL)`, item.ResourceID).Scan(&exists); err != nil || !exists {
					return fmt.Errorf("resource_not_published")
				}
			}
			for _, sha := range []string{req.IconSHA256, req.CoverSHA256} {
				if sha == "" {
					continue
				}
				if len(sha) != 64 {
					return fmt.Errorf("invalid_media_blob")
				}
				if _, err := hex.DecodeString(sha); err != nil {
					return fmt.Errorf("invalid_media_blob")
				}
				var exists bool
				if err := tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM blobs WHERE sha256 = $1 AND media_type = 'image/webp' AND size_bytes <= 10 * 1024 * 1024)`, sha).Scan(&exists); err != nil || !exists {
					return fmt.Errorf("media_blob_not_found")
				}
			}

			// Get next revision_no
			var maxRev int
			_ = tx.QueryRow(r.Context(), `
				SELECT COALESCE(MAX(revision_no), 0) FROM resource_collection_revisions WHERE collection_id = $1
			`, req.ID).Scan(&maxRev)

			nextRev := maxRev + 1
			return tx.QueryRow(r.Context(), `
				INSERT INTO resource_collection_revisions (
					collection_id, revision_no, name, summary, description,
					icon_sha256, cover_sha256, representative_resource_id, items, state
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'draft')
				RETURNING id
			`, req.ID, nextRev, req.Name, req.Summary, req.Description,
				req.IconSHA256, req.CoverSHA256, req.RepresentativeResourceID, req.Items).Scan(&revID)
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "save_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":      "draft_saved",
			"revision_id": revID,
		})
	}
}

func collectionID(r *http.Request) string {
	id := r.PathValue("id")
	if id != "" {
		return id
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, part := range parts {
		if part == "collections" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// HandleCreatorUpdateCollection implements PATCH /api/v1/creator/collections/:id.
func HandleCreatorUpdateCollection(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		uid, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || uid <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		id := collectionID(r)
		var req struct{ Name, Summary string }
		if json.NewDecoder(r.Body).Decode(&req) != nil || id == "" || strings.TrimSpace(req.Name) == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "合集参数错误")
			return
		}
		result, err := s.Pool.Exec(r.Context(), `
			UPDATE resource_collection_revisions cr SET name = $1, summary = $2
			FROM resource_collections c
			WHERE cr.collection_id = c.id AND c.id = $3 AND c.owner_uid = $4 AND cr.state IN ('draft', 'changes_requested')
			AND cr.id = (SELECT id FROM resource_collection_revisions WHERE collection_id = $3 ORDER BY revision_no DESC LIMIT 1)
		`, strings.TrimSpace(req.Name), strings.TrimSpace(req.Summary), id, uid)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "collection_not_found", "合集不存在或没有可编辑版本")
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// HandleCreatorUpdateCollectionResources implements PUT .../:id/resources.
func HandleCreatorUpdateCollectionResources(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		uid, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || uid <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		id := collectionID(r)
		var req struct {
			ResourceIDs              []string `json:"resource_ids"`
			RepresentativeResourceID string   `json:"representative_resource_id"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil || id == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_body", "合集资源参数错误")
			return
		}
		if len(req.ResourceIDs) == 0 && req.RepresentativeResourceID != "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_representative", "空合集不能设置代表资源")
			return
		}
		items := make([]map[string]any, 0, len(req.ResourceIDs))
		seen := map[string]struct{}{}
		for i, resourceID := range req.ResourceIDs {
			resourceID = strings.TrimSpace(resourceID)
			if resourceID == "" {
				httputil.Error(w, http.StatusBadRequest, "invalid_items", "合集资源ID不能为空")
				return
			}
			if _, ok := seen[resourceID]; ok {
				httputil.Error(w, http.StatusBadRequest, "duplicate_item", "合集不能包含重复资源")
				return
			}
			seen[resourceID] = struct{}{}
			var published bool
			if err := s.Pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM resources WHERE resource_id=$1 AND moderation_state='visible' AND current_revision_id IS NOT NULL)`, resourceID).Scan(&published); err != nil || !published {
				httputil.Error(w, http.StatusBadRequest, "resource_not_published", "合集只能包含已发布资源")
				return
			}
			items = append(items, map[string]any{"resource_id": resourceID, "sort_order": i})
		}
		raw, _ := json.Marshal(items)
		result, err := s.Pool.Exec(r.Context(), `
			UPDATE resource_collection_revisions cr SET items = $1, representative_resource_id = $2
			FROM resource_collections c
			WHERE cr.collection_id = c.id AND c.id = $3 AND c.owner_uid = $4 AND cr.state IN ('draft', 'changes_requested')
			AND cr.id = (SELECT id FROM resource_collection_revisions WHERE collection_id = $3 ORDER BY revision_no DESC LIMIT 1)
		`, raw, req.RepresentativeResourceID, id, uid)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "collection_not_found", "合集不存在或没有可编辑版本")
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]string{"status": "updated"})
	}
}

// HandleCreatorDeleteCollection implements DELETE /api/v1/creator/collections/:id.
func HandleCreatorDeleteCollection(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		uid, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil || uid <= 0 {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}
		id := collectionID(r)
		result, err := s.Pool.Exec(r.Context(), `DELETE FROM resource_collections WHERE id = $1 AND owner_uid = $2`, id, uid)
		if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		if result.RowsAffected() == 0 {
			httputil.Error(w, http.StatusNotFound, "collection_not_found", "合集不存在或无权删除")
			return
		}
		httputil.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// HandleCreatorPublishCollection implements POST /api/v1/creator/collections/:id/publish (Spec §12.7)
func HandleCreatorPublishCollection(s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := httputil.ExtractBearerToken(r)
		userUID, err := httputil.AuthenticateUser(r.Context(), s, token)
		if err != nil {
			httputil.Error(w, http.StatusUnauthorized, "unauthorized", "未登录或Token失效")
			return
		}

		id := r.PathValue("id")
		if id == "" {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			for idx, p := range parts {
				if p == "collections" && idx+1 < len(parts) {
					id = parts[idx+1]
					break
				}
			}
		}

		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			var ownerUID int
			if err := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resource_collections WHERE id = $1`, id).Scan(&ownerUID); err != nil {
				return err
			}
			if ownerUID != userUID {
				return fmt.Errorf("forbidden")
			}

			// Find latest draft revision
			var revID int64
			err := tx.QueryRow(r.Context(), `
				SELECT id FROM resource_collection_revisions 
				WHERE collection_id = $1 AND state IN ('draft', 'changes_requested')
				ORDER BY revision_no DESC LIMIT 1
			`, id).Scan(&revID)
			if err != nil {
				return fmt.Errorf("no_draft_revision")
			}

			_, err = tx.Exec(r.Context(), `
				UPDATE resource_collection_revisions SET state = 'waiting_review' WHERE id = $1
			`, revID)
			return err
		})

		if err != nil {
			httputil.Error(w, http.StatusBadRequest, "publish_failed", err.Error())
			return
		}

		httputil.JSON(w, http.StatusOK, map[string]any{
			"status":  "waiting_review",
			"message": "合集提审已提交审核",
		})
	}
}
