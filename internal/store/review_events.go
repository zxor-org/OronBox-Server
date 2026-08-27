package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AdminReviewEvent is one immutable entry in a review case history. The table
// has an update trigger, so an entry can only ever be appended.
type AdminReviewEvent struct {
	ID        string
	Event     string
	ActorID   string
	Actor     string
	Note      string
	Checklist []string
	Detail    map[string]any
	CreatedAt time.Time
}

func (s *Store) AdminReviewEvents(ctx context.Context, caseID string) ([]AdminReviewEvent, error) {
	if _, err := uuid.Parse(caseID); err != nil {
		return nil, ErrAdminReviewNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event.id::text,event.event,COALESCE(event.actor_id::text,''),COALESCE(actor.username,''),event.note,event.checklist,event.detail,event.created_at
FROM review_case_events event
LEFT JOIN users actor ON actor.id=event.actor_id
WHERE event.case_id=$1 ORDER BY event.created_at,event.id`, caseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AdminReviewEvent{}
	for rows.Next() {
		var item AdminReviewEvent
		var checklist, detail []byte
		if err := rows.Scan(&item.ID, &item.Event, &item.ActorID, &item.Actor, &item.Note, &checklist, &detail, &item.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(checklist, &item.Checklist); err != nil {
			return nil, fmt.Errorf("decode review event checklist: %w", err)
		}
		if err := json.Unmarshal(detail, &item.Detail); err != nil {
			return nil, fmt.Errorf("decode review event detail: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Execer lets a review event be appended either on its own connection or
// inside a caller's transaction, so a decision and its history entry commit
// together rather than leaving a decision with no trace behind it.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// AppendReviewCaseEvent writes one history entry. checklist and detail may be
// nil; they are stored as empty JSON rather than null so readers never have to
// branch on it.
func AppendReviewCaseEvent(ctx context.Context, db Execer, caseID, actorID, event, note string, checklist []string, detail map[string]any) error {
	if checklist == nil {
		checklist = []string{}
	}
	if detail == nil {
		detail = map[string]any{}
	}
	checklistJSON, err := json.Marshal(checklist)
	if err != nil {
		return err
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO review_case_events(id,case_id,actor_id,event,note,checklist,detail) VALUES($1,$2,NULLIF($3,'')::uuid,$4,$5,$6,$7)`,
		uuid.NewString(), caseID, actorID, event, note, checklistJSON, detailJSON)
	return err
}

// AppendReviewCaseEventForRevision resolves the case from the revision it
// belongs to, which is the identifier the resource review path carries around.
func AppendReviewCaseEventForRevision(ctx context.Context, db Execer, revisionID, actorID, event, note string, checklist []string, detail map[string]any) error {
	if checklist == nil {
		checklist = []string{}
	}
	if detail == nil {
		detail = map[string]any{}
	}
	checklistJSON, err := json.Marshal(checklist)
	if err != nil {
		return err
	}
	detailJSON, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO review_case_events(id,case_id,actor_id,event,note,checklist,detail)
SELECT $1,review.id,NULLIF($3,'')::uuid,$4,$5,$6,$7 FROM review_cases review WHERE review.revision_id=$2`,
		uuid.NewString(), revisionID, actorID, event, note, checklistJSON, detailJSON)
	return err
}

func (s *Store) RecordReviewCaseEvent(ctx context.Context, caseID, actorID, event, note string, checklist []string, detail map[string]any) error {
	return AppendReviewCaseEvent(ctx, s.db, caseID, actorID, event, note, checklist, detail)
}
