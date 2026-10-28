package coordinator

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type publication struct {
	ID, RevisionID, ResourceID, OwnerID, OwnerName, Target string
	LeaseToken                                             string
	Config                                                 []byte
	StatusDetail                                           []byte
	Attempts                                               int
}

func (c *Coordinator) publishOne(ctx context.Context) error {
	var item publication
	err := c.db.QueryRowContext(ctx, `
WITH candidate AS (
 SELECT p.id FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources resource ON resource.id=rr.resource_id
 WHERE p.state='pending' AND p.target<>'oronbox' AND p.next_attempt_at<=now() AND rr.state='approved'
   AND resource.moderation_state='visible' AND resource.current_revision_id=rr.id
 ORDER BY p.created_at LIMIT 1 FOR UPDATE OF p SKIP LOCKED
), claimed AS (
 UPDATE publications p SET state='running',attempts=attempts+1,updated_at=now() FROM candidate c WHERE p.id=c.id
 RETURNING p.id,p.revision_id,p.target,p.config,p.status_detail,p.attempts
), recorded AS (
 INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,detail)
 SELECT id,attempts,'execute','execution_started','pending','running',jsonb_build_object('target',target) FROM claimed
 RETURNING publication_id
)
SELECT c.id::text,c.revision_id::text,r.id::text,r.owner_id::text,u.username,c.target,c.config,COALESCE(c.status_detail,'{}'::jsonb),c.attempts
FROM claimed c JOIN recorded history ON history.publication_id=c.id JOIN resource_revisions rr ON rr.id=c.revision_id JOIN resources r ON r.id=rr.resource_id JOIN users u ON u.id=r.owner_id`).
		Scan(&item.ID, &item.RevisionID, &item.ResourceID, &item.OwnerID, &item.OwnerName, &item.Target, &item.Config, &item.StatusDetail, &item.Attempts)
	if err != nil {
		return err
	}
	conn, lockErr := c.db.Conn(ctx)
	if lockErr != nil {
		return c.failPublicationAttempt(ctx, item, "pending", lockErr, time.Minute)
	}
	defer conn.Close()
	var acquired bool
	if lockErr = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,7319))`, item.ResourceID).Scan(&acquired); lockErr != nil {
		return c.failPublicationAttempt(ctx, item, "pending", lockErr, time.Minute)
	}
	if !acquired {
		return c.failPublicationAttempt(ctx, item, "pending", fmt.Errorf("resource operation busy"), time.Minute)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,7319))`, item.ResourceID); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	var eligible bool
	if err = c.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources r ON r.id=rr.resource_id WHERE p.id=$1 AND p.state='running' AND rr.state='approved' AND r.current_revision_id=rr.id AND r.moderation_state='visible')`, item.ID).Scan(&eligible); err != nil {
		return err
	}
	if !eligible {
		_, err = c.db.ExecContext(ctx, `UPDATE publications SET state='cancelled',error_message='resource no longer eligible',updated_at=now() WHERE id=$1 AND state='running'`, item.ID)
		return err
	}
	attrs := []any{"publication_id", item.ID, "resource_id", item.ResourceID, "revision_id", item.RevisionID, "target", item.Target, "attempt", item.Attempts}
	if item.Attempts > 1 {
		c.log.Debug("publication started", attrs...)
	} else {
		c.log.Info("publication started", attrs...)
	}
	snapshot, err := c.snapshot(ctx, item.RevisionID, item.ID)
	if err == nil {
		switch item.Target {
		case "bandbbs":
			err = c.publishBandBBS(ctx, item, snapshot)
		case "astrobox":
			err = c.publishAstroBox(ctx, item, snapshot)
		default:
			err = fmt.Errorf("unknown publication target %q", item.Target)
		}
	}
	if err == nil {
		return nil
	}
	state := "pending"
	if item.Attempts >= 5 {
		state = "failed"
	}
	delay := time.Duration(math.Min(3600, math.Pow(2, float64(item.Attempts))*15)) * time.Second
	updateErr := c.failPublicationAttempt(ctx, item, state, err, delay)
	if state == "failed" {
		c.log.Error("publication failed permanently", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "attempts", item.Attempts, "error", err)
	} else {
		c.log.Warn("publication attempt failed", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "attempt", item.Attempts, "retry_in", delay, "error", err)
	}
	return updateErr
}

func (c *Coordinator) failPublicationAttempt(ctx context.Context, item publication, state string, publishErr error, delay time.Duration) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	message := compactError(publishErr)
	result, err := tx.ExecContext(ctx, `UPDATE publications SET state=$2,error_message=$3,next_attempt_at=$4,updated_at=now() WHERE id=$1 AND state='running'`, item.ID, state, message, time.Now().UTC().Add(delay))
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("publication %s is no longer running", item.ID)
	}
	event := "retry_scheduled"
	if state == "failed" {
		event = "execution_failed"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,error_message,detail) VALUES($1,$2,'execute',$3,'running',$4,$5,jsonb_build_object('retry_delay_seconds',$6::bigint))`, item.ID, item.Attempts, event, state, message, int64(delay/time.Second)); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Coordinator) finish(ctx context.Context, item publication, state, externalID, externalURL string, detail map[string]any, meta map[string]string) error {
	raw, _ := json.Marshal(detail)
	metaRaw := []byte("{}")
	if len(meta) > 0 {
		metaRaw, _ = json.Marshal(meta)
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	nextAttempt := time.Time{}
	if state == "reviewing" {
		nextAttempt = time.Now().UTC().Add(pollInterval)
	}
	updateResult, err := tx.ExecContext(ctx, `UPDATE publications SET state=$2,external_id=$3,external_url=$4,error_message='',status_detail=$5,next_attempt_at=CASE WHEN $2='reviewing' THEN $6 ELSE next_attempt_at END,updated_at=now() WHERE id=$1 AND state='running'`, item.ID, state, externalID, externalURL, raw, nextAttempt)
	if err != nil {
		return err
	}
	if err := requirePublicationLease(updateResult, item.ID); err != nil {
		return fmt.Errorf("finish publication: %w", err)
	}
	event := "published"
	if state == "reviewing" {
		event = "submitted_for_review"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,detail) VALUES($1,$2,'execute',$3,'running',$4,jsonb_build_object('external_id',$5::text,'external_url',$6::text,'status_detail',$7::jsonb))`, item.ID, item.Attempts, event, state, externalID, externalURL, raw); err != nil {
		return err
	}
	if item.Target == "astrobox" {
		if _, err = tx.ExecContext(ctx, `UPDATE publications p SET state='cancelled',lease_token=NULL,lease_expires_at=NULL,status_detail=COALESCE(p.status_detail,'{}'::jsonb)||jsonb_build_object('superseded_by',$1::text),updated_at=now() FROM resource_revisions rr WHERE p.revision_id=rr.id AND rr.resource_id=$2 AND p.id<>$1 AND p.target='astrobox' AND p.state='reviewing' AND p.status_detail->>'pull_request_number'=$3`, item.ID, item.ResourceID, fmt.Sprint(detail["pull_request_number"])); err != nil {
			return err
		}
	}
	if item.Target == "bandbbs" {
		if err := c.upsertBandBBSBinding(ctx, tx, item.ResourceID, externalID, externalURL, metaRaw); err != nil {
			return err
		}
		return tx.Commit()
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO external_bindings(id,resource_id,provider,external_id,external_url,meta) VALUES(gen_random_uuid(),$1,$2,$3,$4,$5) ON CONFLICT(resource_id,provider) DO UPDATE SET external_id=excluded.external_id,external_url=excluded.external_url,meta=external_bindings.meta||excluded.meta WHERE external_bindings.external_id=excluded.external_id`, item.ResourceID, item.Target, externalID, externalURL, metaRaw)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("external %s resource %s is already bound to another identity", item.Target, item.ResourceID)
	}
	return tx.Commit()
}

func requirePublicationLease(result sql.Result, publicationID string) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("publication %s polling lease was lost", publicationID)
	}
	return nil
}

func compactError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}
