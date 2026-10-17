package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"time"
)

type DeletionStep struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	State      string `json:"state"`
	Receipt    string `json:"receipt"`
	Error      string `json:"error_message"`
	Attempts   int    `json:"attempts"`
}
type DeletionOperation struct {
	ID         string         `json:"id"`
	ResourceID string         `json:"resource_id"`
	Name       string         `json:"resource_name"`
	State      string         `json:"state"`
	Steps      []DeletionStep `json:"steps"`
}

// Hide locally and record immutable remote targets before any network call.
func (s *Service) Delete(ctx context.Context, ownerID, resourceID string, targets []string) (DeleteResult, error) {
	result := DeleteResult{State: "completed"}
	selected := map[string]bool{}
	for _, p := range targets {
		if p != "bandbbs" && p != "astrobox" {
			return result, ErrInvalid
		}
		selected[p] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var unlocked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended($1,7319))`, resourceID).Scan(&unlocked); err != nil {
		return result, err
	}
	if !unlocked {
		return result, fmt.Errorf("%w: publication is in progress; retry deletion after it finishes", ErrConflict)
	}
	var name string
	var history bool
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(NULLIF((SELECT name FROM resource_revisions WHERE id=r.current_revision_id),''),NULLIF(r.draft_name,''),r.slug),EXISTS(SELECT 1 FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id WHERE rr.resource_id=r.id) FROM resources r WHERE r.id=$1 AND r.owner_id=$2 FOR UPDATE`, resourceID, ownerID).Scan(&name, &history)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM resource_deletion_operations WHERE resource_id=$1`, resourceID).Scan(&existing)
	if err == nil {
		return DeleteResult{OperationID: existing, State: "pending"}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	operation := uuid.NewString()
	raw, _ := json.Marshal(targets)
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_deletion_operations(id,resource_id,owner_id,resource_name,targets)VALUES($1,$2,$3,$4,$5)`, operation, resourceID, ownerID, name, raw); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT provider,external_id FROM external_bindings WHERE resource_id=$1`, resourceID)
	if err != nil {
		return result, err
	}
	type target struct{ provider, id string }
	var steps []target
	for rows.Next() {
		var p, id string
		if err = rows.Scan(&p, &id); err != nil {
			rows.Close()
			return result, err
		}
		if !selected[p] {
			continue
		}
		if p == "bandbbs" {
			for _, remote := range bandBBSResourceIDs(id) {
				steps = append(steps, target{p, remote})
			}
		} else if id != "" {
			steps = append(steps, target{p, id})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, v := range steps {
		if _, err = tx.ExecContext(ctx, `INSERT INTO resource_deletion_steps(id,operation_id,provider,external_id)VALUES($1,$2,$3,$4)`, uuid.NewString(), operation, v.provider, v.id); err != nil {
			return result, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE publications p SET state='cancelled',lease_token=NULL,lease_expires_at=NULL,error_message='resource deleted',updated_at=now() FROM resource_revisions rr WHERE rr.id=p.revision_id AND rr.resource_id=$1 AND p.state IN ('pending','running','reviewing','failed')`, resourceID); err != nil {
		return result, err
	}
	if !history && len(steps) == 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM resources WHERE id=$1`, resourceID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE resources SET moderation_state='deleted',moderation_by='owner',moderation_reason='deleted by owner',moderation_at=now(),updated_at=now(),edit_version=edit_version+1 WHERE id=$1`, resourceID)
	}
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	result.OperationID = operation
	if len(steps) > 0 {
		result.State = "pending"
	}
	return result, nil
}
func (s *Service) DeletionOperation(ctx context.Context, ownerID, id string) (DeletionOperation, error) {
	out := DeletionOperation{State: "completed", Steps: []DeletionStep{}}
	err := s.db.QueryRowContext(ctx, `SELECT id::text,resource_id::text,resource_name FROM resource_deletion_operations WHERE id=$1 AND ($2='' OR owner_id::text=$2)`, id, ownerID).Scan(&out.ID, &out.ResourceID, &out.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,provider,external_id,state,receipt,error_message,attempts FROM resource_deletion_steps WHERE operation_id=$1 ORDER BY provider,external_id`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v DeletionStep
		if err = rows.Scan(&v.ID, &v.Provider, &v.ExternalID, &v.State, &v.Receipt, &v.Error, &v.Attempts); err != nil {
			return out, err
		}
		out.Steps = append(out.Steps, v)
		if v.State != "completed" && out.State != "failed" {
			out.State = v.State
		}
		if v.State == "failed" {
			out.State = "failed"
		}
	}
	return out, rows.Err()
}
func (s *Service) RetryDeletion(ctx context.Context, ownerID, id string) (DeletionOperation, error) {
	op, err := s.DeletionOperation(ctx, ownerID, id)
	if err != nil {
		return op, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE resource_deletion_steps SET state='pending',error_message='',attempts=0,next_attempt_at=now(),updated_at=now() WHERE operation_id=$1 AND state='failed' AND (lease_expires_at IS NULL OR lease_expires_at<=now())`, id)
	if err != nil {
		return op, err
	}
	return s.DeletionOperation(ctx, ownerID, id)
}
func (s *Service) RunDeletionWorker(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.ProcessDeletionStep(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			log(ctx).Error("external deletion step failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) ProcessDeletionStep(ctx context.Context) error {
	var id, owner, provider, external, name, receipt string
	lease := uuid.NewString()
	var attempts int
	err := s.db.QueryRowContext(ctx, `WITH due AS(SELECT id FROM resource_deletion_steps WHERE ((state IN ('pending','awaiting_external') AND next_attempt_at<=now()) OR(state='running' AND lease_expires_at<=now())) ORDER BY next_attempt_at LIMIT 1 FOR UPDATE SKIP LOCKED) UPDATE resource_deletion_steps s SET state='running',lease_token=$1,lease_expires_at=now()+interval '10 minutes',attempts=attempts+1,updated_at=now() FROM due,resource_deletion_operations op WHERE s.id=due.id AND op.id=s.operation_id RETURNING s.id::text,COALESCE(op.owner_id::text,''),s.provider,s.external_id,op.resource_name,s.receipt,s.attempts`, lease).Scan(&id, &owner, &provider, &external, &name, &receipt, &attempts)
	if err != nil {
		return err
	}
	state := "completed"
	if owner == "" {
		err = fmt.Errorf("operation owner unavailable")
	} else if provider == "bandbbs" {
		if s.BandBBSDelete == nil {
			err = fmt.Errorf("BandBBS deletion unavailable")
		} else {
			err = s.BandBBSDelete(ctx, owner, []string{external})
		}
	} else {
		if receipt == "" {
			if s.AstroBoxRemove == nil {
				err = fmt.Errorf("AstroBox removal unavailable")
			} else {
				receipt, err = s.AstroBoxRemove(ctx, owner, external, name)
			}
		}
		if err == nil && receipt != "" {
			if s.AstroBoxRemovalStatus == nil {
				state = "awaiting_external"
			} else {
				state, err = s.AstroBoxRemovalStatus(ctx, owner, receipt)
			}
		}
	}
	message := ""
	if state == "closed_unmerged" {
		state = "failed"
		receipt = ""
		message = "AstroBox removal PR was closed without merging; manual retry will create a new request"
	}
	delay := 5 * time.Minute
	if err != nil {
		state = "failed"
		message = err.Error()
		if attempts < 5 {
			state = "pending"
			delay = time.Duration(attempts*attempts) * time.Minute
		}
	}
	if state != "completed" && state != "pending" && state != "failed" && state != "awaiting_external" {
		state = "failed"
		message = "invalid remote deletion state"
	}
	_, saveErr := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE resource_deletion_steps SET state=$3,receipt=$4,error_message=$5,next_attempt_at=$6,lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND lease_token=$2`, id, lease, state, receipt, message, time.Now().Add(delay))
	if saveErr != nil {
		return saveErr
	}
	return err
}

func (s *Service) DeletionOperations(ctx context.Context, owner string, limit, offset int) ([]DeletionOperation, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM resource_deletion_operations WHERE ($1='' OR owner_id::text=$1) ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, owner, limit, offset)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []DeletionOperation{}
	for _, id := range ids {
		op, err := s.DeletionOperation(ctx, owner, id)
		if err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, nil
}
