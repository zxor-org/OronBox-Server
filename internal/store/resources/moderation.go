package resources

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/model"
)

// AdminManageResource applies an admin moderation action. suspend and freeze
// hide the resource (freeze additionally locks the creator out of editing)
// and cancel its pending/running publications; restore and unfreeze return it
// to visible. delete physically removes a resource that has no revisions.
func (s *Repository) AdminManageResource(ctx context.Context, id, action, reason string, actor model.AdminSession) (AdminResourceActionResult, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminResourceActionResult{}, ErrAdminResourceNotFound
	}
	reason = strings.TrimSpace(reason)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AdminResourceActionResult{}, err
	}
	defer tx.Rollback()
	var result AdminResourceActionResult
	var revisionCount int
	err = tx.QueryRowContext(ctx, `SELECT id::text,slug,owner_id::text,moderation_state,(SELECT count(*) FROM resource_revisions WHERE resource_id=resources.id) FROM resources WHERE id=$1 FOR UPDATE`, id).
		Scan(&result.ID, &result.Slug, &result.OwnerID, &result.PreviousModeration, &revisionCount)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminResourceActionResult{}, ErrAdminResourceNotFound
	}
	if err != nil {
		return AdminResourceActionResult{}, err
	}
	result.ModerationState = result.PreviousModeration
	eventType := ""
	switch action {
	case "suspend":
		result.ModerationState = "suspended"
		eventType = "resource.suspended"
	case "freeze":
		result.ModerationState = "frozen"
		eventType = "resource.frozen"
	case "restore", "unfreeze":
		result.ModerationState = "visible"
		eventType = "resource.restored"
		if action == "unfreeze" {
			eventType = "resource.unfrozen"
		}
	case "delete":
		if revisionCount != 0 {
			return AdminResourceActionResult{}, fmt.Errorf("%w: resources with immutable revisions must be suspended or frozen instead", ErrAdminResourceConflict)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM resources WHERE id=$1`, id); err != nil {
			return AdminResourceActionResult{}, err
		}
		result.Deleted = true
		return result, tx.Commit()
	default:
		return AdminResourceActionResult{}, fmt.Errorf("%w: unknown action", ErrAdminResourceConflict)
	}
	result.Reason = reason
	if result.ModerationState == result.PreviousModeration {
		return result, tx.Commit()
	}
	if result.ModerationState == "visible" {
		if _, err := tx.ExecContext(ctx, `UPDATE resources SET moderation_state='visible',moderation_by=NULL,moderation_reason='',moderation_at=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
			return AdminResourceActionResult{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE resources SET moderation_state=$2,moderation_by='admin',moderation_reason=$3,moderation_at=now(),updated_at=now() WHERE id=$1`, id, result.ModerationState, reason); err != nil {
			return AdminResourceActionResult{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE publications publication SET state='cancelled',error_message='cancelled by resource moderation',updated_at=now() FROM resource_revisions revision WHERE publication.revision_id=revision.id AND revision.resource_id=$1 AND publication.state='pending'`, id); err != nil {
			return AdminResourceActionResult{}, err
		}
	}
	payload, _ := json.Marshal(map[string]any{"admin": actor.Username, "admin_user_id": actor.UserID, "action": action, "reason": reason, "previous_moderation": result.PreviousModeration, "moderation": result.ModerationState})
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_events(resource_id,actor_id,event_type,payload) VALUES($1,NULLIF($2,'')::uuid,$3,$4)`, id, actor.UserID, eventType, payload); err != nil {
		return AdminResourceActionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminResourceActionResult{}, err
	}
	return result, nil
}
