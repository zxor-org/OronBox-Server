package catalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/zxor-org/OronBox-Server/internal/httputil"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

type CollectionItemResource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Summary   string `json:"summary"`
	IconURL   string `json:"icon_url"`
	Version   string `json:"version"`
	CoinCount int    `json:"coin_count"`
}

type CollectionDetailResponse struct {
	ID                       string                   `json:"id"`
	Name                     string                   `json:"name"`
	Summary                  string                   `json:"summary"`
	Description              string                   `json:"description"`
	Owner                    string                   `json:"owner"`
	OwnerUID                 int                      `json:"owner_uid"`
	Kind                     string                   `json:"kind"`
	IconURL                  string                   `json:"icon_url"`
	CoverURL                 string                   `json:"cover_url"`
	CoinCount                int                      `json:"coin_count"`
	ResourceCount            int                      `json:"resource_count"`
	RepresentativeResourceID string                   `json:"representative_resource_id"`
	Resources                []CollectionItemResource `json:"resources"`
}

// HandleGetCollectionDetail implements GET /api/v1/collections/:id (Spec §3.6)
func HandleGetCollectionDetail(s *store.Store, baseURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		if id == "" {
			httputil.Error(w, http.StatusBadRequest, "invalid_id", "缺少合集ID")
			return
		}

		var (
			ownerUID      int
			ownerName     string
			kind          string
			curRevID      *int64
			coinCount     int
			name          string
			summary       string
			desc          string
			iconSHA       string
			coverSHA      string
			repResourceID string
			itemsRaw      json.RawMessage
		)

		err := s.Pool.QueryRow(r.Context(), `
			SELECT c.owner_uid, COALESCE(u.username, ''), c.kind, c.current_revision_id, c.coin_count,
			       cr.name, cr.summary, cr.description, cr.icon_sha256, cr.cover_sha256,
			       COALESCE(cr.representative_resource_id, ''), cr.items
			FROM resource_collections c
			JOIN users u ON c.owner_uid = u.bandbbs_uid
			LEFT JOIN resource_collection_revisions cr ON c.current_revision_id = cr.id
			WHERE c.id = $1 AND c.moderation_state = 'visible'
		`, id).Scan(&ownerUID, &ownerName, &kind, &curRevID, &coinCount,
			&name, &summary, &desc, &iconSHA, &coverSHA, &repResourceID, &itemsRaw)

		if err == pgx.ErrNoRows || curRevID == nil {
			httputil.Error(w, http.StatusNotFound, "collection_not_found", "合集不存在或未公开")
			return
		} else if err != nil {
			httputil.Error(w, http.StatusInternalServerError, "db_error", err.Error())
			return
		}

		type CollItemRef struct {
			ResourceID string `json:"resource_id"`
			SortOrder  int    `json:"sort_order"`
		}
		var itemRefs []CollItemRef
		_ = json.Unmarshal(itemsRaw, &itemRefs)

		var resList []CollectionItemResource
		for _, ref := range itemRefs {
			var it CollectionItemResource
			var icon string
			qErr := s.Pool.QueryRow(r.Context(), `
				SELECT r.resource_id, r.title, r.restype, r.summary, r.icon_sha256,
				       COALESCE(rr.version, '1.0.0'), r.coin_count
				FROM resources r
				LEFT JOIN resource_revisions rr ON r.current_revision_id = rr.id
				WHERE r.resource_id = $1 AND r.moderation_state = 'visible'
			`, ref.ResourceID).Scan(&it.ID, &it.Name, &it.Kind, &it.Summary, &icon, &it.Version, &it.CoinCount)
			if qErr == nil {
				if icon != "" {
					it.IconURL = fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, icon)
				}
				resList = append(resList, it)
			}
		}
		if resList == nil {
			resList = []CollectionItemResource{}
		}

		// Fallback icons if not set on collection
		iconURL := ""
		coverURL := ""
		if iconSHA != "" {
			iconURL = fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, iconSHA)
		} else if len(resList) > 0 && resList[0].IconURL != "" {
			iconURL = resList[0].IconURL
		}
		if coverSHA != "" {
			coverURL = fmt.Sprintf("%s/api/v1/blobs/%s", baseURL, coverSHA)
		}

		httputil.JSON(w, http.StatusOK, CollectionDetailResponse{
			ID:                       id,
			Name:                     name,
			Summary:                  summary,
			Description:              desc,
			Owner:                    ownerName,
			OwnerUID:                 ownerUID,
			Kind:                     kind,
			IconURL:                  iconURL,
			CoverURL:                 coverURL,
			CoinCount:                coinCount,
			ResourceCount:            len(resList),
			RepresentativeResourceID: repResourceID,
			Resources:                resList,
		})
	}
}
