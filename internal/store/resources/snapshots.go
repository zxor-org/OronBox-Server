package resources

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

func (s *Repository) AdminResource(ctx context.Context, id string) (AdminResourceDetail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminResourceDetail{}, ErrAdminResourceNotFound
	}
	page, err := s.AdminResources(ctx, AdminResourceQuery{ResourceID: id, Page: 1, PerPage: 1})
	if err != nil {
		return AdminResourceDetail{}, err
	}
	var summary AdminResourceItem
	for _, item := range page.Items {
		if item.ID == id {
			summary = item
			break
		}
	}
	if summary.ID == "" {
		return AdminResourceDetail{}, ErrAdminResourceNotFound
	}
	detail := AdminResourceDetail{Resource: summary, Revisions: []AdminRevision{}, Bindings: []AdminExternalBinding{}, Events: []AdminResourceEvent{}}
	rows, err := s.db.QueryContext(ctx, `SELECT revision.id::text,revision.revision_no,revision.name,revision.summary,revision.description,revision.description_format,revision.paid_type,
 COALESCE((SELECT jsonb_agg(attribute ORDER BY attribute) FROM resource_revision_attributes WHERE revision_id=revision.id),'[]'::jsonb),revision.publication_plan,revision.state,
 COALESCE(revision.created_by::text,''),revision.created_via,COALESCE(revision.base_revision_id::text,''),
 COALESCE(review.id::text,''),COALESCE(review.state,''),COALESCE(review.note,''),COALESCE(review.items,'[]'::jsonb),
 COALESCE(reviewer.username,''),
 (SELECT count(*) FROM revision_artifacts WHERE revision_id=revision.id),
 (SELECT count(*) FROM revision_media WHERE revision_id=revision.id),
 COALESCE((SELECT jsonb_agg(jsonb_build_object(
	'id',publication.id::text,'target',publication.target,'state',publication.state,
	'config',publication.config,'status_detail',publication.status_detail,
   'external_id',publication.external_id,'external_url',publication.external_url,
	'error_message',publication.error_message,'attempts',publication.attempts,
	'next_attempt_at',publication.next_attempt_at,'updated_at',publication.updated_at
 ) ORDER BY publication.target) FROM publications publication WHERE publication.revision_id=revision.id),'[]'::jsonb),
 revision.created_at,review.updated_at
FROM resource_revisions revision
LEFT JOIN review_cases review ON review.revision_id=revision.id
LEFT JOIN users reviewer ON reviewer.id=review.reviewer_id
WHERE revision.resource_id=$1 ORDER BY revision.revision_no DESC`, id)
	if err != nil {
		return AdminResourceDetail{}, err
	}
	for rows.Next() {
		var revision AdminRevision
		var attributes, reviewItems, publications []byte
		var reviewUpdated sql.NullTime
		if err := rows.Scan(&revision.ID, &revision.Number, &revision.Name, &revision.Summary, &revision.Description, &revision.DescriptionFormat, &revision.PaidType, &attributes, &revision.PublicationPlan, &revision.State, &revision.CreatedBy, &revision.CreatedVia, &revision.BaseRevisionID, &revision.ReviewID, &revision.ReviewState, &revision.ReviewNote, &reviewItems, &revision.Reviewer, &revision.ArtifactCount, &revision.MediaCount, &publications, &revision.CreatedAt, &reviewUpdated); err != nil {
			rows.Close()
			return AdminResourceDetail{}, err
		}
		_ = json.Unmarshal(attributes, &revision.Attributes)
		_ = json.Unmarshal(reviewItems, &revision.ReviewItems)
		if err := json.Unmarshal(publications, &revision.Publications); err != nil {
			rows.Close()
			return AdminResourceDetail{}, fmt.Errorf("decode revision publications: %w", err)
		}
		if reviewUpdated.Valid {
			revision.ReviewUpdatedAt = &reviewUpdated.Time
		}
		detail.Revisions = append(detail.Revisions, revision)
	}
	if err := rows.Close(); err != nil {
		return AdminResourceDetail{}, err
	}
	var artifactQuery, mediaQuery, snapshotID string
	if len(detail.Revisions) > 0 {
		latest := detail.Revisions[0]
		detail.Publications = append(detail.Publications, latest.Publications...)
		snapshotID = latest.ID
		artifactQuery = adminRevisionArtifactsSQL
		mediaQuery = adminRevisionMediaSQL
	}
	if snapshotID != "" {
		detail.Artifacts, err = s.adminArtifacts(ctx, artifactQuery, snapshotID)
		if err != nil {
			return AdminResourceDetail{}, err
		}
		detail.Media, err = s.adminMedia(ctx, mediaQuery, snapshotID)
		if err != nil {
			return AdminResourceDetail{}, err
		}
	}
	bindingRows, err := s.db.QueryContext(ctx, `SELECT provider,external_id,external_url,meta,created_at FROM external_bindings WHERE resource_id=$1 ORDER BY provider`, id)
	if err != nil {
		return AdminResourceDetail{}, err
	}
	for bindingRows.Next() {
		var binding AdminExternalBinding
		var meta []byte
		if err := bindingRows.Scan(&binding.Provider, &binding.ExternalID, &binding.ExternalURL, &meta, &binding.CreatedAt); err != nil {
			bindingRows.Close()
			return AdminResourceDetail{}, err
		}
		binding.present(meta)
		detail.Bindings = append(detail.Bindings, binding)
	}
	if err := bindingRows.Close(); err != nil {
		return AdminResourceDetail{}, err
	}
	eventRows, err := s.db.QueryContext(ctx, `SELECT event.id,COALESCE(actor.username,''),event.event_type,event.payload,event.created_at FROM resource_events event LEFT JOIN users actor ON actor.id=event.actor_id WHERE event.resource_id=$1 ORDER BY event.id DESC LIMIT 200`, id)
	if err != nil {
		return AdminResourceDetail{}, err
	}
	for eventRows.Next() {
		var event AdminResourceEvent
		var payload []byte
		if err := eventRows.Scan(&event.ID, &event.Actor, &event.EventType, &payload, &event.CreatedAt); err != nil {
			eventRows.Close()
			return AdminResourceDetail{}, err
		}
		_ = json.Unmarshal(payload, &event.Payload)
		detail.Events = append(detail.Events, event)
	}
	if err := eventRows.Close(); err != nil {
		return AdminResourceDetail{}, err
	}
	snapshot, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return AdminResourceDetail{}, err
	}
	detail.Snapshot = string(snapshot)
	return detail, nil
}

func (s *Repository) AdminResourceRevision(ctx context.Context, resourceID, revisionID string) (AdminRevisionDetail, error) {
	if _, err := uuid.Parse(resourceID); err != nil {
		return AdminRevisionDetail{}, ErrAdminResourceNotFound
	}
	if _, err := uuid.Parse(revisionID); err != nil {
		return AdminRevisionDetail{}, ErrAdminResourceNotFound
	}
	resource, err := s.AdminResource(ctx, resourceID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	detail := AdminRevisionDetail{Resource: resource.Resource, Artifacts: []AdminArtifact{}, Media: []AdminMedia{}, Links: []AdminLink{}}
	for _, revision := range resource.Revisions {
		if revision.ID == revisionID {
			detail.Revision = revision
			break
		}
	}
	if detail.Revision.ID == "" {
		return AdminRevisionDetail{}, ErrAdminResourceNotFound
	}
	detail.Artifacts, err = s.adminArtifacts(ctx, adminRevisionArtifactsSQL, revisionID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	detail.Media, err = s.adminMedia(ctx, adminRevisionMediaSQL, revisionID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT position,title,url FROM revision_links WHERE revision_id=$1 ORDER BY position`, revisionID)
	if err != nil {
		return AdminRevisionDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var link AdminLink
		if err := rows.Scan(&link.Position, &link.Title, &link.URL); err != nil {
			return AdminRevisionDetail{}, err
		}
		detail.Links = append(detail.Links, link)
	}
	if err := rows.Err(); err != nil {
		return AdminRevisionDetail{}, err
	}
	return detail, nil
}
