package creator

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const creatorReviewPageSize = 30

// ReviewPage returns every review case owned by the current creator. Filtering
// is intentionally left to clients; the cursor only advances the complete,
// stable stream ordered by updated_at and id.
func (s *Service) ReviewPage(ctx context.Context, ownerID, cursor string, limit int) (CreatorReviewPage, error) {
	if limit <= 0 || limit > 100 {
		limit = creatorReviewPageSize
	}
	where := "resource.owner_id=$1"
	args := []any{ownerID}
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return CreatorReviewPage{}, fmt.Errorf("%w: invalid review cursor", ErrInvalid)
		}
		parts := strings.SplitN(string(decoded), "|", 2)
		if len(parts) != 2 {
			return CreatorReviewPage{}, fmt.Errorf("%w: invalid review cursor", ErrInvalid)
		}
		when, err := time.Parse(time.RFC3339Nano, parts[0])
		if err != nil {
			return CreatorReviewPage{}, fmt.Errorf("%w: invalid review cursor", ErrInvalid)
		}
		where += " AND (review.updated_at, review.id) < ($2,$3)"
		args = append(args, when, parts[1])
	}
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, `
SELECT review.id::text,revision.id::text,revision.resource_id::text,revision.name,
       resource.kind,review.state,review.note,review.updated_at,
       CASE WHEN EXISTS (SELECT 1 FROM publications publication
                         WHERE publication.revision_id=revision.id AND publication.target='astrobox')
            THEN 'astrobox' ELSE 'normal' END
FROM review_cases review
JOIN resource_revisions revision ON revision.id=review.revision_id
JOIN resources resource ON resource.id=revision.resource_id
WHERE `+where+`
ORDER BY review.updated_at DESC,review.id DESC
LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return CreatorReviewPage{}, err
	}
	defer rows.Close()
	items := make([]CreatorReviewListItem, 0, limit)
	for rows.Next() {
		var item CreatorReviewListItem
		if err := rows.Scan(&item.ID, &item.RevisionID, &item.ResourceID, &item.ResourceName, &item.ResourceKind, &item.State, &item.Note, &item.UpdatedAt, &item.ReviewType); err != nil {
			return CreatorReviewPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return CreatorReviewPage{}, err
	}
	page := CreatorReviewPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(last.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID))
		page.HasMore = true
	}
	return page, nil
}

// ReviewWorkspace resolves a review case to the owner's normal workspace so
// new clients can inspect a review without changing the legacy workspace API.
func (s *Service) ReviewWorkspace(ctx context.Context, ownerID, reviewID string) (Workspace, error) {
	var resourceID string
	err := s.db.QueryRowContext(ctx, `SELECT revision.resource_id::text FROM review_cases review JOIN resource_revisions revision ON revision.id=review.revision_id JOIN resources resource ON resource.id=revision.resource_id WHERE review.id=$1 AND resource.owner_id=$2`, reviewID, ownerID).Scan(&resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	workspace, err := s.Workspace(ctx, ownerID, resourceID)
	if err != nil {
		return Workspace{}, err
	}
	var review ReviewCase
	err = s.db.QueryRowContext(ctx, `SELECT id::text,revision_id::text,state,note,created_at,updated_at FROM review_cases WHERE id=$1`, reviewID).Scan(&review.ID, &review.RevisionID, &review.State, &review.Note, &review.CreatedAt, &review.UpdatedAt)
	if err != nil {
		return Workspace{}, err
	}
	workspace.Review = &review
	workspace.Publications, err = s.publications(ctx, review.RevisionID)
	return workspace, err
}

// AstroBoxPublication returns the creator-owned AstroBox publication binding
// needed by the server-side GitHub proxy. The client never supplies repository
// or pull request identifiers.
func (s *Service) AstroBoxPublication(ctx context.Context, ownerID, reviewID string) (map[string]any, error) {
	var repo, externalURL, detail string
	var prID string
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(p.status_detail->>'pull_request_repository',p.status_detail->>'repository',''),COALESCE(p.external_url,''),
       COALESCE(p.status_detail->>'pull_request_number',''),COALESCE(p.status_detail::text,'{}')
FROM review_cases review JOIN resource_revisions rev ON rev.id=review.revision_id
JOIN resources resource ON resource.id=rev.resource_id
JOIN publications p ON p.revision_id=rev.id AND p.target='astrobox'
WHERE review.id=$1 AND resource.owner_id=$2
ORDER BY p.updated_at DESC LIMIT 1`, reviewID, ownerID).Scan(&repo, &externalURL, &prID, &detail)
	if err != nil {
		return nil, err
	}
	if parsed, parseErr := url.Parse(externalURL); parseErr == nil && parsed.Host == "github.com" {
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) >= 4 && (parts[2] == "pull" || parts[2] == "pulls") {
			repo = parts[0] + "/" + parts[1]
		}
	}
	return map[string]any{"repository": repo, "external_url": externalURL, "pull_request_number": prID, "status_detail": json.RawMessage(detail)}, nil
}

func (s *Service) ReviewQueue(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT r.owner_id::text,r.id::text FROM review_cases c JOIN resource_revisions rr ON rr.id=c.revision_id JOIN resources r ON r.id=rr.resource_id WHERE c.state='pending' ORDER BY r.id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pair struct{ owner, resource string }
	var pairs []pair
	for rows.Next() {
		var item pair
		if err := rows.Scan(&item.owner, &item.resource); err != nil {
			return nil, err
		}
		pairs = append(pairs, item)
	}
	result := make([]Workspace, 0, len(pairs))
	for _, item := range pairs {
		workspace, err := s.Workspace(ctx, item.owner, item.resource)
		if err != nil {
			return nil, err
		}
		result = append(result, workspace)
	}
	return result, rows.Err()
}
