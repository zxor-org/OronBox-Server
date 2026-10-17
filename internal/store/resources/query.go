package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (query AdminResourceQuery) normalized() AdminResourceQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Owner = strings.TrimSpace(query.Owner)
	query.ResourceID = strings.TrimSpace(query.ResourceID)
	query.Device = strings.TrimSpace(query.Device)
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = 25
	}
	if query.PerPage > 100 {
		query.PerPage = 100
	}
	if query.Kind != "quickapp" && query.Kind != "watchface" {
		query.Kind = ""
	}
	if query.Moderation != "visible" && query.Moderation != "suspended" && query.Moderation != "frozen" {
		query.Moderation = ""
	}
	if query.RevisionState != "submitted" && query.RevisionState != "approved" && query.RevisionState != "rejected" && query.RevisionState != "superseded" {
		query.RevisionState = ""
	}
	if query.CurrentRevisionState != "submitted" && query.CurrentRevisionState != "approved" && query.CurrentRevisionState != "rejected" && query.CurrentRevisionState != "superseded" {
		query.CurrentRevisionState = ""
	}
	if query.ReviewState != "pending" && query.ReviewState != "approved" && query.ReviewState != "rejected" && query.ReviewState != "superseded" {
		query.ReviewState = ""
	}
	if query.PublicationTarget != "oronbox" && query.PublicationTarget != "bandbbs" && query.PublicationTarget != "astrobox" {
		query.PublicationTarget = ""
	}
	if query.PublicationState != "pending" && query.PublicationState != "running" && query.PublicationState != "published" && query.PublicationState != "reviewing" && query.PublicationState != "failed" && query.PublicationState != "cancelled" {
		query.PublicationState = ""
	}
	switch query.Sort {
	case "updated_asc", "created_desc", "name", "owner":
	default:
		query.Sort = "updated_desc"
	}
	return query
}

func (s *Repository) AdminResources(ctx context.Context, raw AdminResourceQuery) (AdminResourcePage, error) {
	query := raw.normalized()
	args := make([]any, 0, 12)
	where := []string{"TRUE"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.ResourceID != "" {
		add(`r.id=?`, query.ResourceID)
	}
	if query.Search != "" {
		add(`(r.id::text ILIKE '%'||?||'%' OR r.slug ILIKE '%'||?||'%' OR u.username ILIKE '%'||?||'%' OR COALESCE(latest.name,'') ILIKE '%'||?||'%' OR COALESCE(latest.summary,'') ILIKE '%'||?||'%')`, query.Search)
	}
	if query.Owner != "" {
		add(`(u.username ILIKE '%'||?||'%' OR r.owner_id::text=?)`, query.Owner)
	}
	if query.Kind != "" {
		add(`r.kind=?`, query.Kind)
	}
	if query.Device != "" {
		add(`EXISTS(
 SELECT 1 FROM resource_revisions device_revision
 JOIN revision_artifacts device_artifact ON device_artifact.revision_id=device_revision.id
 JOIN revision_artifact_devices device_binding ON device_binding.artifact_id=device_artifact.id
 JOIN devices filter_device ON filter_device.id=device_binding.device_id
 WHERE device_revision.resource_id=r.id AND (filter_device.id::text=? OR filter_device.codename=?)
)`, query.Device)
	}
	if query.Moderation != "" {
		add(`r.moderation_state=?`, query.Moderation)
	}
	if query.RevisionState != "" {
		add(`latest.state=?`, query.RevisionState)
	}
	if query.CurrentRevisionState != "" {
		add(`current_revision.state=?`, query.CurrentRevisionState)
	}
	if query.ReviewState != "" {
		add(`review.state=?`, query.ReviewState)
	}
	if query.PublicationTarget != "" && query.PublicationState != "" {
		args = append(args, query.PublicationTarget, query.PublicationState)
		where = append(where, fmt.Sprintf(`EXISTS(
 SELECT 1 FROM publications filter_publication
 WHERE filter_publication.revision_id=latest.id
   AND filter_publication.target=$%d
   AND filter_publication.state=$%d
)`, len(args)-1, len(args)))
	} else if query.PublicationTarget != "" {
		add(`EXISTS(SELECT 1 FROM publications filter_publication WHERE filter_publication.revision_id=latest.id AND filter_publication.target=?)`, query.PublicationTarget)
	} else if query.PublicationState != "" {
		add(`EXISTS(SELECT 1 FROM publications filter_publication WHERE filter_publication.revision_id=latest.id AND filter_publication.state=?)`, query.PublicationState)
	}
	order := "r.updated_at DESC, r.id DESC"
	switch query.Sort {
	case "updated_asc":
		order = "r.updated_at ASC, r.id ASC"
	case "created_desc":
		order = "r.created_at DESC, r.id DESC"
	case "name":
		order = "COALESCE(latest.name,r.slug) ASC, r.id ASC"
	case "owner":
		order = "u.username ASC, r.updated_at DESC"
	}
	base := `FROM resources r
JOIN users u ON u.id=r.owner_id
LEFT JOIN resource_revisions current_revision ON current_revision.id=r.current_revision_id
LEFT JOIN LATERAL (SELECT revision.* FROM resource_revisions revision WHERE revision.resource_id=r.id ORDER BY revision.revision_no DESC LIMIT 1) latest ON TRUE
LEFT JOIN review_cases review ON review.revision_id=latest.id
WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+base, args...).Scan(&total); err != nil {
		return AdminResourcePage{}, err
	}
	args = append(args, query.PerPage, (query.Page-1)*query.PerPage)
	limitPosition, offsetPosition := len(args)-1, len(args)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT r.id::text,r.owner_id::text,u.username,r.slug,r.platform,r.kind,r.moderation_state,COALESCE(r.moderation_by,''),r.moderation_reason,r.moderation_at,
 COALESCE(current_revision.id::text,''),COALESCE(current_revision.revision_no,0),COALESCE(current_revision.name,''),COALESCE(current_revision.state,''),
 COALESCE(latest.id::text,''),COALESCE(latest.revision_no,0),COALESCE(latest.name,''),COALESCE(latest.state,''),
 COALESCE(review.state,''),
 COALESCE((SELECT jsonb_agg(jsonb_build_object(
   'id',publication.id::text,'target',publication.target,'state',publication.state,
   'external_id',publication.external_id,'external_url',publication.external_url,
   'error_message',publication.error_message,'attempts',publication.attempts,'updated_at',publication.updated_at
 ) ORDER BY publication.target) FROM publications publication WHERE publication.revision_id=latest.id),'[]'::jsonb),
 r.created_at,r.updated_at
%s
ORDER BY %s LIMIT $%d OFFSET $%d`, base, order, limitPosition, offsetPosition), args...)
	if err != nil {
		return AdminResourcePage{}, err
	}
	defer rows.Close()
	page := AdminResourcePage{Page: query.Page, PerPage: query.PerPage, Query: query, Items: []AdminResourceItem{}, Total: total}
	for rows.Next() {
		var item AdminResourceItem
		var publications []byte
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Owner, &item.Slug, &item.Platform, &item.Kind, &item.ModerationState, &item.ModerationBy, &item.ModerationReason, &item.ModerationAt, &item.CurrentRevisionID, &item.CurrentRevisionNumber, &item.CurrentRevisionName, &item.CurrentRevisionState, &item.LatestRevisionID, &item.LatestRevisionNumber, &item.LatestRevisionName, &item.LatestRevisionState, &item.LatestReviewState, &publications, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return AdminResourcePage{}, err
		}
		if err := json.Unmarshal(publications, &item.Publications); err != nil {
			return AdminResourcePage{}, fmt.Errorf("decode resource publications: %w", err)
		}
		item.Name = item.LatestRevisionName
		if item.Name == "" {
			item.Name = item.Slug
		}
		item.RevisionNo = item.LatestRevisionNumber
		item.RevisionState = item.LatestRevisionState
		item.ReviewState = item.LatestReviewState
		for _, publication := range item.Publications {
			item.Targets = append(item.Targets, publication.Target)
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminResourcePage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}
