package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (s *Service) Review(ctx context.Context, revisionID, reviewerID string, approve bool, note string, items, attributes []string, grade string) error {
	grade = strings.TrimSpace(grade)
	if grade != "standard" && grade != "featured" {
		return fmt.Errorf("%w: curation grade", ErrInvalid)
	}
	note = strings.TrimSpace(note)
	// A rejection the creator cannot act on just produces another identical
	// submission, so the reason is required here rather than only in the form.
	if !approve && note == "" {
		return fmt.Errorf("%w: rejection reason is required", ErrInvalid)
	}
	seenAttributes := make(map[string]bool, len(attributes))
	confirmedAttributes := make([]string, 0, len(attributes))
	for _, attribute := range attributes {
		attribute = strings.TrimSpace(attribute)
		if attribute != "" && !seenAttributes[attribute] {
			seenAttributes[attribute] = true
			confirmedAttributes = append(confirmedAttributes, attribute)
		}
	}
	attributes = confirmedAttributes
	if approve && (len(attributes) > 32 || !s.attributesExist(ctx, attributes, true)) {
		return fmt.Errorf("%w: resource attributes", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	outcome, err := s.decideReview(ctx, tx, revisionID, reviewerID, approve, note, items, attributes, grade)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	outcome.log(ctx, revisionID, reviewerID, note, approve)
	return nil
}

// ReviewBatch applies one decision to several review cases in a single
// transaction. It is all or nothing: if any case has already moved on, nothing
// is written, so a reviewer never has to work out which half of a bulk action
// landed.
//
// Bulk approval keeps each case's existing checklist, attributes and curation
// grade. Those are per-resource judgements that a queue-level action has no
// basis to overwrite.
func (s *Service) ReviewBatch(ctx context.Context, caseIDs []string, reviewerID string, approve bool, note string) error {
	note = strings.TrimSpace(note)
	if !approve && note == "" {
		return fmt.Errorf("%w: rejection reason is required", ErrInvalid)
	}
	if len(caseIDs) == 0 || len(caseIDs) > 100 {
		return fmt.Errorf("%w: batch size", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT review.revision_id::text,review.items,resource.curation_grade,
 COALESCE((SELECT jsonb_agg(attribute ORDER BY attribute) FROM resource_revision_attributes WHERE revision_id=review.revision_id),'[]'::jsonb)
FROM review_cases review
JOIN resource_revisions revision ON revision.id=review.revision_id
JOIN resources resource ON resource.id=revision.resource_id
WHERE review.id=ANY($1::uuid[]) AND review.state='pending'`, caseIDs)
	if err != nil {
		return err
	}
	type pending struct {
		revisionID string
		grade      string
		items      []string
		attributes []string
	}
	var cases []pending
	for rows.Next() {
		var item pending
		var itemsJSON, attributesJSON []byte
		if err := rows.Scan(&item.revisionID, &itemsJSON, &item.grade, &attributesJSON); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(itemsJSON, &item.items); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal(attributesJSON, &item.attributes); err != nil {
			rows.Close()
			return err
		}
		cases = append(cases, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(cases) != len(caseIDs) {
		return ErrConflict
	}
	outcomes := make([]reviewOutcome, 0, len(cases))
	for _, item := range cases {
		outcome, err := s.decideReview(ctx, tx, item.revisionID, reviewerID, approve, note, item.items, item.attributes, item.grade)
		if err != nil {
			return err
		}
		outcomes = append(outcomes, outcome)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for index, outcome := range outcomes {
		outcome.log(ctx, cases[index].revisionID, reviewerID, note, approve)
	}
	return nil
}

// reviewOutcome carries what the transaction learned so the caller can log it
// after the commit rather than logging a decision that might still roll back.
type reviewOutcome struct {
	resourceID string
	revisionNo int
	restored   bool
}

func (outcome reviewOutcome) log(ctx context.Context, revisionID, reviewerID, note string, approve bool) {
	message := "revision rejected"
	if approve {
		message = "revision approved"
	}
	log(ctx).Info(message, "resource_id", outcome.resourceID, "revision_id", revisionID, "revision_no", outcome.revisionNo, "reviewer_id", reviewerID, "has_note", strings.TrimSpace(note) != "")
	if outcome.restored {
		log(ctx).Info("resource restored by review approval", "resource_id", outcome.resourceID, "revision_id", revisionID, "reviewer_id", reviewerID)
	}
}

// decideReview applies one review decision inside the caller's transaction so
// a batch of decisions can commit or roll back as a single unit.
func (s *Service) decideReview(ctx context.Context, tx *sql.Tx, revisionID, reviewerID string, approve bool, note string, items, attributes []string, grade string) (reviewOutcome, error) {
	var err error
	state, revisionState := ReviewRejected, RevisionRejected
	if approve {
		state, revisionState = ReviewApproved, RevisionApproved
	}
	itemsJSON, _ := json.Marshal(items)
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET state=$2,reviewer_id=NULLIF($3,'')::uuid,note=$4,items=$5,updated_at=now() WHERE revision_id=$1 AND state='pending'`, revisionID, state, reviewerID, note, itemsJSON)
	if err != nil {
		return reviewOutcome{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return reviewOutcome{}, ErrConflict
	}
	var resourceID string
	var revisionNo int
	var createdVia string
	if err = tx.QueryRowContext(ctx, `UPDATE resource_revisions SET state=$2 WHERE id=$1 RETURNING resource_id::text,revision_no,created_via`, revisionID, revisionState).Scan(&resourceID, &revisionNo, &createdVia); err != nil {
		return reviewOutcome{}, err
	}
	var acquired bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,7319))`, resourceID).Scan(&acquired); err != nil {
		return reviewOutcome{}, err
	}
	if !acquired {
		return reviewOutcome{}, fmt.Errorf("%w: publication is in progress", ErrConflict)
	}
	var restoredByReview bool
	if approve {
		if _, err = tx.ExecContext(ctx, `DELETE FROM resource_revision_attributes WHERE revision_id=$1`, revisionID); err != nil {
			return reviewOutcome{}, err
		}
		for _, attribute := range attributes {
			if _, err = tx.ExecContext(ctx, `INSERT INTO resource_revision_attributes(revision_id,attribute) VALUES($1,$2)`, revisionID, attribute); err != nil {
				return reviewOutcome{}, err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resources SET curation_grade=$2,updated_at=now(),edit_version=edit_version+1 WHERE id=$1`, resourceID, grade); err != nil {
			return reviewOutcome{}, err
		}
		if _, err = tx.ExecContext(ctx, `
UPDATE resource_revisions previous
SET state='superseded'
FROM resources resource,resource_revisions approved
WHERE approved.id=$1
  AND resource.id=approved.resource_id
  AND previous.id=resource.current_revision_id
  AND previous.id<>approved.id
  AND previous.state='approved'`, revisionID); err != nil {
			return reviewOutcome{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resources r SET current_revision_id=$1,published_at=now(),updated_at=now() FROM resource_revisions rr WHERE rr.id=$1 AND r.id=rr.resource_id`, revisionID); err != nil {
			return reviewOutcome{}, err
		}
		if createdVia == "admin" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO resource_sources(resource_id,author_name,source_url,license_name,authorization_note) SELECT resource_id,COALESCE(governance_source->>'author_name',''),COALESCE(governance_source->>'source_url',''),COALESCE(governance_source->>'license_name',''),COALESCE(governance_source->>'authorization_note','') FROM resource_revisions WHERE id=$1 ON CONFLICT(resource_id) DO UPDATE SET author_name=excluded.author_name,source_url=excluded.source_url,license_name=excluded.license_name,authorization_note=excluded.authorization_note,updated_at=now()`, revisionID); err != nil {
				return reviewOutcome{}, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE resources resource SET collection_id=revision.governance_collection_id,collection_position=revision.governance_collection_position FROM resource_revisions revision WHERE revision.id=$1 AND resource.id=revision.resource_id`, revisionID); err != nil {
				return reviewOutcome{}, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM resource_collaborators WHERE resource_id=$1`, resourceID); err != nil {
				return reviewOutcome{}, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collaborators(resource_id,user_id,invited_by,accepted_at) SELECT revision.resource_id,collaborator.user_id,COALESCE(NULLIF($2,'')::uuid,resource.owner_id),now() FROM resource_revision_collaborators collaborator JOIN resource_revisions revision ON revision.id=collaborator.revision_id JOIN resources resource ON resource.id=revision.resource_id WHERE collaborator.revision_id=$1`, revisionID, reviewerID); err != nil {
				return reviewOutcome{}, err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE publications p SET approved_snapshot=to_jsonb(r) FROM resource_revisions r WHERE p.revision_id=r.id AND r.id=$1 AND p.approved_snapshot='{}'::jsonb`, revisionID); err != nil {
			return reviewOutcome{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE publications SET state=CASE WHEN target='oronbox' THEN 'published' ELSE 'pending' END,updated_at=now() WHERE revision_id=$1`, revisionID); err != nil {
			return reviewOutcome{}, err
		}
		// An approved revision makes the resource sellable again: lift a
		// suspension (owner takedown or admin action) so it returns to the
		// public catalog. Frozen resources stay locked until an admin acts.
		restored, err := tx.ExecContext(ctx, `UPDATE resources SET moderation_state='visible',moderation_by=NULL,moderation_reason='',moderation_at=NULL,updated_at=now() WHERE id=$1 AND moderation_state='suspended'`, resourceID)
		if err != nil {
			return reviewOutcome{}, err
		}
		if count, _ := restored.RowsAffected(); count == 1 {
			restoredByReview = true
			if err = event(ctx, tx, resourceID, reviewerID, "resource.restored", map[string]any{"trigger": "review_approved", "revision_id": revisionID, "revision_no": revisionNo, "previous_moderation": "suspended"}); err != nil {
				return reviewOutcome{}, err
			}
		}
	} else if _, err = tx.ExecContext(ctx, `UPDATE publications SET state='cancelled',updated_at=now() WHERE revision_id=$1`, revisionID); err != nil {
		return reviewOutcome{}, err
	}
	decision := "rejected"
	if approve {
		decision = "approved"
	}
	if err = store.AppendReviewCaseEventForRevision(ctx, tx, revisionID, reviewerID, decision, note, items, map[string]any{
		"revision_id": revisionID, "revision_no": revisionNo, "resource_id": resourceID,
		"curation_grade": grade, "attributes": attributes, "restored": restoredByReview,
	}); err != nil {
		return reviewOutcome{}, err
	}
	return reviewOutcome{resourceID: resourceID, revisionNo: revisionNo, restored: restoredByReview}, nil
}
