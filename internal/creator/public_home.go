package creator

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// PublicHomeFeed builds all dynamic resource rows from oversampled candidate
// pools. Keeping the selection here prevents the client from rendering the
// same card in Featured, Recommended, and Latest at the same time.
func (s *Service) PublicHomeFeed(ctx context.Context, query PublicHomeQuery) (PublicHomeFeed, error) {
	rowSize := query.RowSize
	if rowSize <= 0 || rowSize > 24 {
		rowSize = 12
	}
	candidateLimit := rowSize * 8
	if candidateLimit < 48 {
		candidateLimit = 48
	}
	if candidateLimit > 100 {
		candidateLimit = 100
	}
	seed := query.Seed
	if seed == 0 {
		seed = time.Now().UTC().Unix() / 86400
	}
	featured, err := s.publicResourceRows(ctx, PublicQuery{Limit: candidateLimit, Seed: seed, Featured: true, Sort: "recommendation"})
	if err != nil {
		return PublicHomeFeed{}, err
	}
	recommended, err := s.publicResourceRows(ctx, PublicQuery{Limit: candidateLimit, Seed: seed, Sort: "recommendation"})
	if err != nil {
		return PublicHomeFeed{}, err
	}
	latest, err := s.publicResourceRows(ctx, PublicQuery{Limit: candidateLimit, Seed: seed, Sort: "time", SkipRecommendationScore: true})
	if err != nil {
		return PublicHomeFeed{}, err
	}

	excluded := make(map[string]struct{}, len(query.ExcludeIDs))
	for _, id := range query.ExcludeIDs {
		if id = strings.TrimSpace(id); id != "" {
			excluded[id] = struct{}{}
		}
	}
	seen := make(map[string]struct{}, rowSize*3)
	cardKey := func(item PublicResource) string {
		return item.CardType + ":" + item.ID
	}
	selectRow := func(candidates []PublicResource, allow func(PublicResource) bool, maxItemsPerOwner int) []PublicResource {
		result := make([]PublicResource, 0, rowSize)
		ownerCounts := make(map[string]int)
		for _, item := range candidates {
			key := cardKey(item)
			if _, ok := seen[key]; ok || !allow(item) {
				continue
			}
			owner := strings.TrimSpace(item.Owner)
			if maxItemsPerOwner > 0 && owner != "" && ownerCounts[owner] >= maxItemsPerOwner {
				continue
			}
			seen[key] = struct{}{}
			if owner != "" {
				ownerCounts[owner]++
			}
			result = append(result, item)
			if len(result) == rowSize {
				break
			}
		}
		if len(result) < rowSize && maxItemsPerOwner > 0 {
			for _, item := range candidates {
				key := cardKey(item)
				if _, ok := seen[key]; ok || !allow(item) {
					continue
				}
				seen[key] = struct{}{}
				result = append(result, item)
				if len(result) == rowSize {
					break
				}
			}
		}
		return result
	}
	// Exclusions are resource ids, while row de-duplication uses a typed key
	// because resources and collections are stored in separate tables.
	allow := func(item PublicResource) bool {
		_, excludedID := excluded[item.ID]
		return !excludedID
	}
	return PublicHomeFeed{
		// Match the client-facing order so duplicates stay in the first
		// section users see.
		Latest:      selectRow(latest, allow, 2),
		Recommended: selectRow(recommended, allow, 2),
		Featured:    selectRow(featured, allow, 0),
	}, nil
}

// HomeResourceCard is the compact resource payload rendered on the home
// feed, with up to three preview images for the wide card layout.
type HomeResourceCard struct {
	ID          string           `json:"id"`
	Slug        string           `json:"slug"`
	Name        string           `json:"name"`
	Summary     string           `json:"summary"`
	Kind        string           `json:"kind"`
	PaidType    ResourcePaidType `json:"paid_type"`
	Owner       string           `json:"owner"`
	IconSHA256  string           `json:"icon_sha256"`
	CoverSHA256 string           `json:"cover_sha256"`
	Previews    []string         `json:"previews"`
}

// HomeResources returns visible resource cards for the given IDs, preserving
// the caller's order. Missing or hidden resources are skipped.
func (s *Service) HomeResources(ctx context.Context, ids []string) ([]HomeResourceCard, error) {
	if len(ids) == 0 {
		return []HomeResourceCard{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id::text,r.slug,rr.name,rr.summary,r.kind,rr.paid_type,u.username,
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='icon' ORDER BY position LIMIT 1),''),
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='cover' ORDER BY position LIMIT 1),''),
COALESCE((SELECT jsonb_agg(blob_sha256 ORDER BY position) FROM (SELECT blob_sha256,position FROM revision_media WHERE revision_id=rr.id AND role='preview' ORDER BY position LIMIT 3) p),'[]')
FROM resources r JOIN resource_revisions rr ON rr.id=r.current_revision_id JOIN users u ON u.id=r.owner_id
WHERE r.moderation_state='visible' AND r.id=ANY($1::uuid[])
ORDER BY array_position($1::uuid[],r.id)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]HomeResourceCard, 0, len(ids))
	for rows.Next() {
		var item HomeResourceCard
		var previews []byte
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Summary, &item.Kind, &item.PaidType, &item.Owner, &item.IconSHA256, &item.CoverSHA256, &previews); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(previews, &item.Previews)
		result = append(result, item)
	}
	return result, rows.Err()
}
