package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
)

func (c *Coordinator) pollOne(ctx context.Context) error {

	var item publication
	var detail []byte
	err := c.db.QueryRowContext(ctx, `WITH candidate AS (
 SELECT p.id FROM publications p WHERE p.target='astrobox' AND p.state='reviewing' AND p.next_attempt_at<=now() AND (p.lease_token IS NULL OR p.lease_expires_at<=now())
 ORDER BY p.next_attempt_at,p.id LIMIT 1 FOR UPDATE OF p SKIP LOCKED
), claimed AS (
 UPDATE publications p SET lease_token=gen_random_uuid(),lease_expires_at=now()+interval '5 minutes',updated_at=now() FROM candidate c WHERE p.id=c.id
 RETURNING p.id,p.revision_id,p.target,p.config,p.attempts,p.status_detail,p.lease_token
)
SELECT p.id::text,p.revision_id::text,r.id::text,r.owner_id::text,u.username,p.target,p.config,p.attempts,p.status_detail,p.lease_token::text
FROM claimed p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources r ON r.id=rr.resource_id JOIN users u ON u.id=r.owner_id`).Scan(&item.ID, &item.RevisionID, &item.ResourceID, &item.OwnerID, &item.OwnerName, &item.Target, &item.Config, &item.Attempts, &detail, &item.LeaseToken)
	if err != nil {
		return err
	}
	retired, err := c.db.ExecContext(ctx, `UPDATE publications old SET state='cancelled',lease_token=NULL,lease_expires_at=NULL,status_detail=COALESCE(old.status_detail,'{}'::jsonb)||jsonb_build_object('superseded_by',new.id::text),updated_at=now() FROM publications new,resource_revisions a,resource_revisions b WHERE old.id=$1 AND old.lease_token=$2 AND old.revision_id=a.id AND new.revision_id=b.id AND a.resource_id=b.resource_id AND b.revision_no>a.revision_no AND old.state='reviewing' AND new.target='astrobox' AND new.state IN ('reviewing','published') AND old.status_detail->>'pull_request_number'=new.status_detail->>'pull_request_number'`, item.ID, item.LeaseToken)
	if err != nil {
		return err
	}
	if count, err := retired.RowsAffected(); err != nil {
		return err
	} else if count > 0 {
		return nil
	}
	var statusDetail struct {
		PullRequestNumber  int       `json:"pull_request_number"`
		SubmissionProtocol string    `json:"submission_protocol"`
		CatalogRow         []string  `json:"catalog_row"`
		ReopenRequestedAt  time.Time `json:"reopen_requested_at"`
	}
	if json.Unmarshal(detail, &statusDetail) != nil || statusDetail.PullRequestNumber <= 0 {
		err = fmt.Errorf("publication %s has no pull request number", item.ID)
		return c.failPublicationPoll(ctx, item, detail, err)
	}
	token, err := c.githubToken(ctx, item.OwnerID)
	if err != nil {
		return c.failPublicationPoll(ctx, item, detail, err)
	}
	status, err := c.astro.PullRequest(ctx, token, statusDetail.PullRequestNumber)
	if err != nil {
		if astrobox.IsUnauthorized(err) {
			c.rejectGitHubToken(ctx, item.OwnerID, token)
		}
		return c.failPublicationPoll(ctx, item, detail, err)
	}
	pollDetail := pollStatusDetail(detail, status.State, status.URL, "")
	reopening := !statusDetail.ReopenRequestedAt.IsZero() && time.Since(statusDetail.ReopenRequestedAt) < 2*time.Minute
	if status.State == "open" {
		var extra map[string]any
		_ = json.Unmarshal(pollDetail, &extra)
		delete(extra, "reopen_requested_at")
		pollDetail, _ = json.Marshal(extra)
	}
	if !status.Merged && status.State == "open" {
		timeline, timelineErr := c.astro.ReviewTimeline(ctx, token, statusDetail.PullRequestNumber)
		if timelineErr != nil {
			return c.failPublicationPoll(ctx, item, detail, timelineErr)
		}
		var extra map[string]any
		_ = json.Unmarshal(pollDetail, &extra)
		extra["review_state"], extra["review_items"] = timeline.State, timeline.ReviewItems
		pollDetail, _ = json.Marshal(extra)
	}
	catalogApplied := true
	var catalogCheck astrobox.CatalogCheck
	if status.Merged && statusDetail.SubmissionProtocol == "v2" {
		if len(statusDetail.CatalogRow) != 12 {
			err = fmt.Errorf("AstroBox v2 publication has no expected catalog row")
			return c.failPublicationPoll(ctx, item, detail, err)
		}
		catalogCheck, err = c.astro.CheckCatalogEntry(ctx, token, statusDetail.CatalogRow[0], statusDetail.CatalogRow)
		if err != nil {
			return c.failPublicationPoll(ctx, item, detail, err)
		}
		catalogApplied = catalogCheck.Matches
		pollDetail = pollCatalogStatusDetail(pollDetail, catalogCheck)
	}
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if status.Merged && catalogApplied {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE publications SET state='published',external_url=$2,error_message='',status_detail=$3,lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND state='reviewing' AND lease_token=$4`, item.ID, status.URL, pollDetail, item.LeaseToken)
		if err == nil {
			err = requirePublicationLease(result, item.ID)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,detail) VALUES($1,$2,'poll','published','reviewing','published',jsonb_build_object('external_url',$3::text,'pull_request_state',$4::text))`, item.ID, item.Attempts, status.URL, status.State)
		}
		if err == nil {
			c.log.Info("publication completed", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_url", status.URL)
		}
	} else if !status.Merged && status.State == "closed" && !reopening {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE publications SET state='failed',error_message='外部审核不通过',status_detail=$2,lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND state='reviewing' AND lease_token=$3`, item.ID, pollDetail, item.LeaseToken)
		if err == nil {
			err = requirePublicationLease(result, item.ID)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,error_message,detail) VALUES($1,$2,'poll','external_review_rejected','reviewing','failed','外部审核不通过',jsonb_build_object('external_url',$3::text,'pull_request_state',$4::text))`, item.ID, item.Attempts, status.URL, status.State)
		}
		if err == nil {
			c.log.Warn("publication failed permanently", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_url", status.URL, "error", "external review rejected")
		}
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE publications SET error_message='',status_detail=$2,next_attempt_at=$3,lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND state='reviewing' AND lease_token=$4`, item.ID, pollDetail, nextPollTime(reopening), item.LeaseToken)
		if err == nil {
			err = requirePublicationLease(result, item.ID)
		}
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (c *Coordinator) failPublicationPoll(ctx context.Context, item publication, detail []byte, pollErr error) error {
	message := compactError(pollErr)
	pollDetail := pollStatusDetail(detail, "", "", message)
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE publications SET error_message=$2,status_detail=$3,next_attempt_at=$4,lease_token=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND state='reviewing' AND lease_token=$5`, item.ID, message, pollDetail, time.Now().UTC().Add(pollInterval), item.LeaseToken)
	if err != nil {
		return err
	}
	if err := requirePublicationLease(result, item.ID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func pollStatusDetail(raw []byte, state, externalURL, pollError string) []byte {
	detail := map[string]any{}
	if err := json.Unmarshal(raw, &detail); err != nil || detail == nil {
		detail = map[string]any{}
	}
	count := 0
	if value, ok := detail["poll_count"].(float64); ok && value >= 0 {
		count = int(value)
	}
	detail["poll_count"] = count + 1
	detail["last_polled_at"] = time.Now().UTC().Format(time.RFC3339)
	if state != "" {
		detail["last_poll_state"] = state
	}
	if externalURL != "" {
		detail["last_poll_url"] = externalURL
	}
	if pollError == "" {
		delete(detail, "last_poll_error")
	} else {
		detail["last_poll_error"] = compactError(errors.New(pollError))
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return raw
	}
	return encoded
}

func pollCatalogStatusDetail(raw []byte, check astrobox.CatalogCheck) []byte {
	detail := map[string]any{}
	if err := json.Unmarshal(raw, &detail); err != nil || detail == nil {
		detail = map[string]any{}
	}
	state := "pending"
	if check.Matches {
		state = "applied"
	} else if check.Found {
		state = "mismatch"
	}
	detail["catalog_state"] = state
	if check.Found {
		detail["catalog_row_found"] = true
	} else {
		detail["catalog_row_found"] = false
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return raw
	}
	return encoded
}

func nextPollTime(reopening bool) time.Time {
	interval := pollInterval
	if reopening {
		interval = 15 * time.Second
	}
	return time.Now().UTC().Add(interval)
}
