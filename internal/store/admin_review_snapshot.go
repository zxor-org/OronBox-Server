package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/reviewdiff"
)

func (s *Store) AdminReview(ctx context.Context, id string) (AdminReviewDetail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminReviewDetail{}, ErrAdminReviewNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT
 review.id::text,review.state,review.note,review.items,COALESCE(review.reviewer_id::text,''),COALESCE(reviewer.username,''),
 resource.id::text,resource.slug,resource.kind,resource.moderation_state,resource.platform,resource.curation_grade,
 revision.id::text,revision.revision_no,revision.name,revision.state,
 owner.id::text,owner.username,
 COALESCE((SELECT jsonb_agg(publication.target ORDER BY publication.target) FROM publications publication WHERE publication.revision_id=revision.id),'[]'::jsonb),
 review.created_at,review.updated_at,
 review.priority,review.due_at,review.first_submitted_at,
 (SELECT count(*) FROM review_cases history JOIN resource_revisions history_revision ON history_revision.id=history.revision_id JOIN resources history_resource ON history_resource.id=history_revision.resource_id WHERE history_resource.owner_id=resource.owner_id AND history.state='rejected'),
 (SELECT count(*) FROM feedback_tickets report WHERE report.kind='resource_report' AND report.target_id=resource.id::text AND report.status IN ('open','investigating'))
FROM review_cases review
JOIN resource_revisions revision ON revision.id=review.revision_id
JOIN resources resource ON resource.id=revision.resource_id
JOIN users owner ON owner.id=resource.owner_id
LEFT JOIN users reviewer ON reviewer.id=review.reviewer_id
WHERE review.id=$1`, id)
	item, err := scanAdminReview(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminReviewDetail{}, ErrAdminReviewNotFound
	}
	if err != nil {
		return AdminReviewDetail{}, err
	}
	detail := AdminReviewDetail{Review: item}
	detail.Current, err = s.adminReviewRevisionSnapshot(ctx, item.RevisionID)
	if err != nil {
		return AdminReviewDetail{}, err
	}
	baseID := detail.Current.BaseRevisionID
	if baseID == "" {
		err = s.db.QueryRowContext(ctx, `SELECT id::text FROM resource_revisions WHERE resource_id=$1 AND revision_no<$2 ORDER BY revision_no DESC LIMIT 1`, item.ResourceID, item.RevisionNumber).Scan(&baseID)
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
		if err != nil {
			return AdminReviewDetail{}, err
		}
	}
	if baseID != "" {
		detail.Base, err = s.adminReviewRevisionSnapshot(ctx, baseID)
		if err != nil {
			return AdminReviewDetail{}, err
		}
	}
	detail.Diff = summarizeAdminReviewDiff(detail.Base, detail.Current)
	return detail, nil
}

func (s *Store) adminReviewRevisionSnapshot(ctx context.Context, id string) (AdminReviewRevisionSnapshot, error) {
	var snapshot AdminReviewRevisionSnapshot
	var publicationPlan, attributes []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision.id::text,revision.revision_no,revision.name,revision.summary,revision.description,revision.description_format,revision.paid_type,revision.state,
 COALESCE(revision.created_by::text,''),revision.created_via,COALESCE(revision.base_revision_id::text,''),revision.publication_plan,
 COALESCE((SELECT jsonb_agg(attribute ORDER BY attribute) FROM resource_revision_attributes WHERE revision_id=revision.id),'[]'::jsonb),revision.created_at
FROM resource_revisions revision WHERE revision.id=$1`, id).Scan(
		&snapshot.ID, &snapshot.Number, &snapshot.Name, &snapshot.Summary, &snapshot.Description, &snapshot.DescriptionFormat, &snapshot.PaidType, &snapshot.State,
		&snapshot.CreatedBy, &snapshot.CreatedVia, &snapshot.BaseRevisionID, &publicationPlan, &attributes, &snapshot.CreatedAt,
	)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	if err := json.Unmarshal(publicationPlan, &snapshot.PublicationPlan); err != nil {
		return AdminReviewRevisionSnapshot{}, fmt.Errorf("decode revision publication plan: %w", err)
	}
	if err := json.Unmarshal(attributes, &snapshot.Attributes); err != nil {
		return AdminReviewRevisionSnapshot{}, fmt.Errorf("decode revision attributes: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT position,title,url FROM revision_links WHERE revision_id=$1 ORDER BY position`, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	for rows.Next() {
		var link AdminLink
		if err := rows.Scan(&link.Position, &link.Title, &link.URL); err != nil {
			rows.Close()
			return AdminReviewRevisionSnapshot{}, err
		}
		snapshot.Links = append(snapshot.Links, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return AdminReviewRevisionSnapshot{}, err
	}
	if err := rows.Close(); err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	snapshot.Media, err = s.resourceRepository().RevisionMedia(ctx, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	snapshot.Artifacts, err = s.resourceRepository().RevisionArtifacts(ctx, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	snapshot.Governance, err = s.AdminRevisionGovernance(ctx, id)
	if err != nil {
		return AdminReviewRevisionSnapshot{}, err
	}
	return snapshot, nil
}

func linkValues(items []AdminLink) map[string]string {
	values := map[string]string{}
	for _, item := range items {
		values[fmt.Sprintf("%d", item.Position)] = item.Title + "\x00" + item.URL
	}
	return values
}

func mediaValues(items []AdminMedia) map[string]string {
	values := map[string]string{}
	for _, item := range items {
		key := fmt.Sprintf("%s:%d", item.Role, item.Position)
		values[key] = fmt.Sprintf("%s:%d:%d:%d", item.SHA256, item.Width, item.Height, item.SizeBytes)
	}
	return values
}

func artifactValues(items []AdminArtifact) map[string]string {
	values := map[string]string{}
	for _, item := range items {
		devices := append([]string(nil), item.Devices...)
		sort.Strings(devices)
		analysis, _ := json.Marshal(item.Analysis)
		key := item.PackageID + "\x00" + item.OriginalName
		values[key] = fmt.Sprintf("%s:%s:%s:%d:%s:%s", item.SHA256, item.PackageFormat, item.Version, item.SizeBytes, analysis, strings.Join(devices, "\x00"))
	}
	return values
}

func artifactDevices(items []AdminArtifact) []string {
	seen := map[string]struct{}{}
	for _, item := range items {
		for _, device := range item.Devices {
			seen[device] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for device := range seen {
		values = append(values, device)
	}
	return values
}

func summarizeAdminReviewDiff(base, current AdminReviewRevisionSnapshot) AdminReviewDiff {
	return reviewdiff.Compare(reviewDiffSnapshot(base), reviewDiffSnapshot(current))
}

func reviewDiffSnapshot(value AdminReviewRevisionSnapshot) reviewdiff.Snapshot {
	return reviewdiff.Snapshot{ID: value.ID, Name: value.Name, Summary: value.Summary, Description: value.Description, DescriptionFormat: value.DescriptionFormat, PaidType: value.PaidType, PublicationPlan: value.PublicationPlan, Governance: value.Governance, Attributes: value.Attributes, Devices: artifactDevices(value.Artifacts), Links: linkValues(value.Links), Media: mediaValues(value.Media), Artifacts: artifactValues(value.Artifacts)}
}

func (s *Store) AdminReviewers(ctx context.Context) ([]AdminReviewerOption, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,username,role FROM users WHERE role IN ('reviewer','admin') AND banned_at IS NULL ORDER BY username,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AdminReviewerOption{}
	for rows.Next() {
		var item AdminReviewerOption
		if err := rows.Scan(&item.ID, &item.Username, &item.Role); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeReviewChecklist(items []string) []string {
	result, seen := make([]string, 0, len(items)), map[string]bool{}
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

func (s *Store) AdminSaveReviewChecklist(ctx context.Context, reviewID, actorID string, items []string) error {
	if _, err := uuid.Parse(reviewID); err != nil {
		return ErrAdminReviewNotFound
	}
	checklist := normalizeReviewChecklist(items)
	raw, _ := json.Marshal(checklist)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET items=$2,updated_at=now() WHERE id=$1 AND state='pending'`, reviewID, raw)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrAdminReviewConflict
	}
	if err := AppendReviewCaseEvent(ctx, tx, reviewID, actorID, "checklist_saved", "", checklist, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizeReviewIDs(ids []string) ([]string, error) {
	result, seen := make([]string, 0, len(ids)), map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, err := uuid.Parse(id); err != nil {
			return nil, ErrAdminReviewNotFound
		}
		seen[id] = true
		result = append(result, id)
	}
	if len(result) == 0 || len(result) > 100 {
		return nil, ErrAdminReviewConflict
	}
	return result, nil
}

func (s *Store) AdminAssignReviews(ctx context.Context, ids []string, reviewerID, actorID string) error {
	ids, err := normalizeReviewIDs(ids)
	if err != nil {
		return err
	}
	if reviewerID != "" {
		if _, err := uuid.Parse(reviewerID); err != nil {
			return ErrAdminReviewConflict
		}
		var valid bool
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND role IN ('reviewer','admin') AND banned_at IS NULL)`, reviewerID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrAdminReviewConflict
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET reviewer_id=NULLIF($2,'')::uuid,updated_at=now() WHERE id=ANY($1::uuid[]) AND state='pending'`, ids, reviewerID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); int(count) != len(ids) {
		return ErrAdminReviewConflict
	}
	event := "assigned"
	if reviewerID == "" {
		event = "unassigned"
	}
	for _, id := range ids {
		if err := AppendReviewCaseEvent(ctx, tx, id, actorID, event, "", nil, map[string]any{"reviewer_id": reviewerID}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// AdminSetReviewPriority retunes the queue. The SLA deadline moves with the
// priority so an urgent case does not keep a deadline set for a routine one.
func (s *Store) AdminSetReviewPriority(ctx context.Context, ids []string, priority int, actorID string) error {
	ids, err := normalizeReviewIDs(ids)
	if err != nil {
		return err
	}
	if priority < 0 || priority > 3 {
		return ErrAdminReviewConflict
	}
	hours := map[int]int{0: 48, 1: 24, 2: 12, 3: 4}[priority]
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET priority=$2,due_at=COALESCE(first_submitted_at,created_at)+make_interval(hours=>$3),updated_at=now() WHERE id=ANY($1::uuid[]) AND state='pending'`, ids, priority, hours)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); int(count) != len(ids) {
		return ErrAdminReviewConflict
	}
	for _, id := range ids {
		if err := AppendReviewCaseEvent(ctx, tx, id, actorID, "priority_changed", "", nil, map[string]any{"priority": priority, "sla_hours": hours}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
