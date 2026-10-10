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
		if req.Items == nil {
			req.Items = json.RawMessage("[]")
		}

		var revID int64
		err = s.WithTx(r.Context(), func(tx pgx.Tx) error {
			// Ensure collection exists and owned by user
			var ownerUID int
			qErr := tx.QueryRow(r.Context(), `SELECT owner_uid FROM resource_collections WHERE id = $1`, req.ID).Scan(&ownerUID)
			if qErr == pgx.ErrNoRows {
				// Bug 2.5: moderation_state must be 'suspended' (valid: visible, suspended, deleted)
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
				WHERE collection_id = $1 AND state = 'draft' 
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
