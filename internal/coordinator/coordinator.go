package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/config"
	bandbbsoauth "github.com/zxor-org/OronBox-Server/internal/oauth/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/publish/astrobox"
	"github.com/zxor-org/OronBox-Server/internal/publish/bandbbs"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

const interval = 3 * time.Second

type Coordinator struct {
	db       *sql.DB
	store    *store.Store
	local    blob.Store
	r2       *blob.R2
	secrets  *auth.Secrets
	cfg      config.Config
	bandAuth *bandbbsoauth.Client
	band     *bandbbs.Client
	astro    *astrobox.Client
	log      *slog.Logger
}

func New(db *sql.DB, storage *store.Store, local blob.Store, r2 *blob.R2, secrets *auth.Secrets, cfg config.Config, bandAuth *bandbbsoauth.Client) *Coordinator {
	return &Coordinator{
		db: db, store: storage, local: local, r2: r2, secrets: secrets, cfg: cfg, bandAuth: bandAuth,
		band:  bandbbs.New(cfg.BandBBS.APIURL, local, storage),
		astro: astrobox.New(cfg.AstroBox, cfg.GitHub.APIURL, local, storage),
		log:   observability.For("coordinator"),
	}
}

func (c *Coordinator) Run(ctx context.Context) {
	c.log.Info("resource coordinator started", "r2_enabled", c.r2 != nil)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer c.log.Info("resource coordinator stopped")
	nextMaintenance := time.Time{}
	for {
		c.tick(ctx)
		if time.Now().After(nextMaintenance) {
			if err := c.maintain(ctx); err != nil && !errors.Is(err, context.Canceled) {
				c.log.Error("resource maintenance failed", "error", err)
			}
			nextMaintenance = time.Now().Add(c.cfg.Retention.Interval)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *Coordinator) maintain(ctx context.Context) error {
	_, _ = c.db.ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at<now()`)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM login_tickets WHERE expires_at<now()`)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM github_device_flows WHERE expires_at<now()`)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM sessions WHERE refresh_expires_at<now()`)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM resources r WHERE r.current_revision_id IS NULL AND NOT EXISTS(SELECT 1 FROM resource_revisions revision WHERE revision.resource_id=r.id) AND r.updated_at<$1`, time.Now().UTC().Add(-c.cfg.Retention.Unpublished))
	_, _ = c.db.ExecContext(ctx, `DELETE FROM audit_logs WHERE created_at<$1`, time.Now().UTC().Add(-c.cfg.Retention.Audit))
	_, _ = c.db.ExecContext(ctx, `DELETE FROM feedback_tickets WHERE status IN ('resolved','dismissed','closed') AND updated_at<$1`, time.Now().UTC().Add(-c.cfg.Retention.Feedback))
	for count := 0; count < 100; count++ {
		tx, err := c.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var sha, localKey, r2Key string
		err = tx.QueryRowContext(ctx, `
SELECT b.sha256,b.local_key,COALESCE(replica.object_key,'') FROM blobs b
LEFT JOIN blob_replicas replica ON replica.blob_sha256=b.sha256 AND replica.backend='r2'
WHERE b.created_at<$1
AND NOT EXISTS(SELECT 1 FROM revision_media WHERE blob_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM revision_artifacts WHERE blob_sha256=b.sha256)
ORDER BY b.created_at LIMIT 1 FOR UPDATE OF b SKIP LOCKED`, time.Now().UTC().Add(-c.cfg.Retention.OrphanBlobs)).Scan(&sha, &localKey, &r2Key)
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return nil
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if r2Key != "" && c.r2 != nil {
			if err := c.r2.Delete(ctx, r2Key); err != nil {
				tx.Rollback()
				return fmt.Errorf("delete orphan R2 blob %s: %w", sha, err)
			}
		}
		if err := c.local.Delete(ctx, localKey); err != nil {
			tx.Rollback()
			return fmt.Errorf("delete orphan local blob %s: %w", sha, err)
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM blobs WHERE sha256=$1
AND NOT EXISTS(SELECT 1 FROM revision_media WHERE blob_sha256=$1)
AND NOT EXISTS(SELECT 1 FROM revision_artifacts WHERE blob_sha256=$1)`, sha)
		if err != nil {
			tx.Rollback()
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) tick(ctx context.Context) {
	if c.r2 != nil {
		if err := c.pruneR2Replica(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
			c.log.Error("R2 visibility reconciliation failed", "error", err)
		}
		if err := c.replicateOne(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
			c.log.Error("R2 replication failed", "error", err)
		}
	}
	if err := c.publishOne(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
		c.log.Error("resource publication failed", "error", err)
	}
	if err := c.pollOne(ctx); err != nil && !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, context.Canceled) {
		c.log.Error("AstroBox publication status check failed", "error", err)
	}
}

func (c *Coordinator) pruneR2Replica(ctx context.Context) error {
	var sha, key string
	err := c.db.QueryRowContext(ctx, `
WITH candidate AS (
 SELECT replica.blob_sha256
 FROM blob_replicas replica
 WHERE replica.backend='r2'
   AND replica.state='ready'
   AND NOT EXISTS (
    SELECT 1
    FROM resources resource
    WHERE resource.moderation_state='visible'
      AND resource.current_revision_id IS NOT NULL
      AND (
       EXISTS (
        SELECT 1 FROM revision_artifacts artifact
        WHERE artifact.revision_id=resource.current_revision_id
          AND artifact.blob_sha256=replica.blob_sha256
       )
       OR EXISTS (
        SELECT 1 FROM revision_media media
        WHERE media.revision_id=resource.current_revision_id
          AND media.blob_sha256=replica.blob_sha256
       )
      )
   )
 ORDER BY replica.updated_at
 LIMIT 1
 FOR UPDATE SKIP LOCKED
)
UPDATE blob_replicas replica
SET state='uploading',updated_at=now()
FROM candidate
WHERE replica.blob_sha256=candidate.blob_sha256 AND replica.backend='r2'
RETURNING replica.blob_sha256,replica.object_key`).Scan(&sha, &key)
	if err != nil {
		return err
	}
	if key != "" {
		if err := c.r2.Delete(ctx, key); err != nil {
			_, _ = c.db.ExecContext(ctx, `UPDATE blob_replicas SET state='ready',error_message=$2,updated_at=now() WHERE blob_sha256=$1 AND backend='r2'`, sha, compactError(err))
			return err
		}
	}
	if _, err := c.db.ExecContext(ctx, `DELETE FROM blob_replicas WHERE blob_sha256=$1 AND backend='r2'`, sha); err != nil {
		_, _ = c.db.ExecContext(ctx, `UPDATE blob_replicas SET state='failed',error_message=$2,next_attempt_at=now(),updated_at=now() WHERE blob_sha256=$1 AND backend='r2'`, sha, compactError(err))
		return err
	}
	c.log.Info("R2 replica removed from public storage", "sha256", sha)
	return nil
}

func (c *Coordinator) replicateOne(ctx context.Context) error {
	var sha, key, mediaType string
	var size int64
	err := c.db.QueryRowContext(ctx, `
WITH candidate AS (
 SELECT b.sha256 FROM blobs b LEFT JOIN blob_replicas r ON r.blob_sha256=b.sha256 AND r.backend='r2'
 WHERE (
  r.blob_sha256 IS NULL
  OR (r.state='failed' AND r.next_attempt_at<=now())
  OR (r.state='uploading' AND r.updated_at<now()-interval '15 minutes')
  OR (
   r.state='ready'
   AND r.object_key<>(
    'sha256/'||substr(b.sha256,1,2)||'/'||substr(b.sha256,3,2)||'/'||b.sha256
   )
  )
 )
 AND EXISTS (
  SELECT 1 FROM resources resource
  WHERE resource.moderation_state='visible'
    AND resource.current_revision_id IS NOT NULL
    AND (
     EXISTS (
      SELECT 1 FROM revision_artifacts artifact
      WHERE artifact.revision_id=resource.current_revision_id AND artifact.blob_sha256=b.sha256
     )
     OR EXISTS (
      SELECT 1 FROM revision_media media
      WHERE media.revision_id=resource.current_revision_id AND media.blob_sha256=b.sha256
     )
    )
 )
 ORDER BY b.created_at LIMIT 1 FOR UPDATE OF b SKIP LOCKED
), claimed AS (
 INSERT INTO blob_replicas(blob_sha256,backend,state,attempts,next_attempt_at)
 SELECT sha256,'r2','uploading',1,now() FROM candidate
 ON CONFLICT(blob_sha256,backend) DO UPDATE SET state='uploading',attempts=blob_replicas.attempts+1,updated_at=now()
 RETURNING blob_sha256
)
SELECT b.sha256,b.local_key,b.media_type,b.size_bytes FROM blobs b JOIN claimed c ON c.blob_sha256=b.sha256`).Scan(&sha, &key, &mediaType, &size)
	if err != nil {
		return err
	}
	file, err := c.local.Open(ctx, key)
	if err == nil {
		defer file.Close()
		err = c.r2.Put(ctx, blob.SHA256Key(sha), mediaType, size, file)
	}
	if err != nil {
		_, _ = c.db.ExecContext(ctx, `UPDATE blob_replicas SET state='failed',error_message=$2,next_attempt_at=now()+make_interval(secs=>LEAST(3600,power(2,attempts)::int*15)),updated_at=now() WHERE blob_sha256=$1 AND backend='r2'`, sha, compactError(err))
		return err
	}
	_, err = c.db.ExecContext(ctx, `UPDATE blob_replicas SET state='ready',object_key=$2,error_message='',updated_at=now() WHERE blob_sha256=$1 AND backend='r2'`, sha, blob.SHA256Key(sha))
	if err == nil {
		c.log.Info("R2 replica completed", "sha256", sha, "bytes", size, "media_type", mediaType)
	}
	return err
}

type publication struct {
	ID, RevisionID, ResourceID, OwnerID, OwnerName, Target string
	Config                                                 []byte
	Attempts                                               int
}

func (c *Coordinator) publishOne(ctx context.Context) error {
	var item publication
	err := c.db.QueryRowContext(ctx, `
WITH candidate AS (
 SELECT p.id FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources resource ON resource.id=rr.resource_id
 WHERE p.state='pending' AND p.target<>'oronbox' AND p.next_attempt_at<=now() AND rr.state='approved'
   AND resource.moderation_state='visible'
 ORDER BY p.created_at LIMIT 1 FOR UPDATE OF p SKIP LOCKED
), claimed AS (
 UPDATE publications p SET state='running',attempts=attempts+1,updated_at=now() FROM candidate c WHERE p.id=c.id
 RETURNING p.id,p.revision_id,p.target,p.config,p.attempts
)
SELECT c.id::text,c.revision_id::text,r.id::text,r.owner_id::text,u.username,c.target,c.config,c.attempts
FROM claimed c JOIN resource_revisions rr ON rr.id=c.revision_id JOIN resources r ON r.id=rr.resource_id JOIN users u ON u.id=r.owner_id`).
		Scan(&item.ID, &item.RevisionID, &item.ResourceID, &item.OwnerID, &item.OwnerName, &item.Target, &item.Config, &item.Attempts)
	if err != nil {
		return err
	}
	attrs := []any{"publication_id", item.ID, "resource_id", item.ResourceID, "revision_id", item.RevisionID, "target", item.Target, "attempt", item.Attempts}
	if item.Attempts > 1 {
		c.log.Debug("publication started", attrs...)
	} else {
		c.log.Info("publication started", attrs...)
	}
	snapshot, err := c.snapshot(ctx, item.RevisionID)
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
	_, updateErr := c.db.ExecContext(ctx, `UPDATE publications SET state=$2,error_message=$3,next_attempt_at=$4,updated_at=now() WHERE id=$1`, item.ID, state, compactError(err), time.Now().UTC().Add(delay))
	if state == "failed" {
		c.log.Error("publication failed permanently", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "attempts", item.Attempts, "error", err)
	} else {
		c.log.Warn("publication attempt failed", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "attempt", item.Attempts, "retry_in", delay, "error", err)
	}
	return updateErr
}

func (c *Coordinator) publishBandBBS(ctx context.Context, item publication, snapshot []byte) error {
	token, err := c.bandBBSToken(ctx, item.OwnerID)
	if err != nil {
		return err
	}
	var publishConfig struct {
		Targets []struct {
			CategoryID int `json:"category_id"`
		} `json:"targets"`
	}
	_ = json.Unmarshal(item.Config, &publishConfig)
	var bound string
	_ = c.db.QueryRowContext(ctx, `SELECT external_id FROM external_bindings WHERE resource_id=$1 AND provider='bandbbs'`, item.ResourceID).Scan(&bound)
	targetIDs := make([]int, 0, len(publishConfig.Targets))
	for _, target := range publishConfig.Targets {
		targetIDs = append(targetIDs, target.CategoryID)
	}
	existing, err := parseBandBBSBinding(bound, targetIDs)
	if err != nil {
		return err
	}
	if bound != "" {
		normalized, marshalErr := json.Marshal(existing)
		if marshalErr != nil {
			return marshalErr
		}
		if string(normalized) != bound {
			if _, err := c.db.ExecContext(ctx, `UPDATE external_bindings SET external_id=$2 WHERE resource_id=$1 AND provider='bandbbs'`, item.ResourceID, normalized); err != nil {
				return err
			}
			bound = string(normalized)
		}
	}
	result, err := c.band.Publish(ctx, token, existing, snapshot, item.Config)
	if err != nil {
		return err
	}
	canonical := make(map[string]string, len(result.Resources))
	for categoryID, resource := range result.Resources {
		canonical[categoryID] = resource.ResourceID
	}
	externalID, _ := json.Marshal(canonical)
	externalURL := ""
	if len(publishConfig.Targets) > 0 {
		externalURL = result.Resources[strconv.Itoa(publishConfig.Targets[0].CategoryID)].URL
	}
	if err := c.finish(ctx, item, "published", string(externalID), externalURL, map[string]any{"resources": result.Resources}, nil); err != nil {
		return err
	}
	c.log.Info("publication completed", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_url", externalURL, "categories", len(result.Resources))
	return nil
}

func parseBandBBSBinding(raw string, targetCategoryIDs []int) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	canonical := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &canonical); err == nil {
		return canonical, nil
	}
	var historical map[string]struct {
		ResourceID string `json:"resource_id"`
	}
	if err := json.Unmarshal([]byte(raw), &historical); err == nil {
		result := make(map[string]string, len(historical))
		for categoryID, resource := range historical {
			if strings.TrimSpace(resource.ResourceID) == "" {
				return nil, fmt.Errorf("invalid BandBBS binding for category %s", categoryID)
			}
			result[categoryID] = resource.ResourceID
		}
		return result, nil
	}
	if len(targetCategoryIDs) == 1 {
		if _, err := strconv.ParseUint(raw, 10, 64); err == nil {
			return map[string]string{strconv.Itoa(targetCategoryIDs[0]): raw}, nil
		}
	}
	return nil, fmt.Errorf("invalid BandBBS binding; refusing to create duplicate resources")
}

func (c *Coordinator) publishAstroBox(ctx context.Context, item publication, snapshot []byte) error {
	token, err := c.githubToken(ctx, item.OwnerID)
	if err != nil {
		return err
	}
	// Imported resources carry their existing AstroBox identity in the
	// binding; fill any gaps in the publication config from it so the
	// publication updates that repo instead of creating a duplicate.
	var publishConfig struct {
		ItemID   string `json:"item_id"`
		RepoName string `json:"repo_name"`
	}
	if err := json.Unmarshal(item.Config, &publishConfig); err == nil && (strings.TrimSpace(publishConfig.ItemID) == "" || strings.TrimSpace(publishConfig.RepoName) == "") {
		var boundID, boundMeta string
		bindErr := c.db.QueryRowContext(ctx, `SELECT external_id,meta::text FROM external_bindings WHERE resource_id=$1 AND provider='astrobox'`, item.ResourceID).Scan(&boundID, &boundMeta)
		if bindErr == nil {
			config := map[string]any{}
			_ = json.Unmarshal(item.Config, &config)
			meta := map[string]string{}
			_ = json.Unmarshal([]byte(boundMeta), &meta)
			if strings.TrimSpace(publishConfig.ItemID) == "" && boundID != "" {
				config["item_id"] = boundID
			}
			if strings.TrimSpace(publishConfig.RepoName) == "" && meta["repo_name"] != "" {
				config["repo_name"] = meta["repo_name"]
			}
			if merged, marshalErr := json.Marshal(config); marshalErr == nil {
				item.Config = merged
			}
		}
	}
	result, err := c.astro.Publish(ctx, token, item.OwnerName, snapshot, item.Config)
	if err != nil {
		return err
	}
	detail := map[string]any{"pull_request_number": result.PullRequestNumber, "repository": result.Repository}
	var astroConfig struct {
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(item.Config, &astroConfig); err != nil || strings.TrimSpace(astroConfig.ItemID) == "" {
		return fmt.Errorf("AstroBox item_id is missing")
	}
	meta := map[string]string{}
	if name := strings.TrimPrefix(result.Repository, "https://github.com/"); name != result.Repository {
		if slash := strings.IndexByte(name, '/'); slash > 0 {
			meta["repo_owner"], meta["repo_name"] = name[:slash], name[slash+1:]
		}
	}
	if err := c.finish(ctx, item, "reviewing", astroConfig.ItemID, result.PullRequest, detail, meta); err != nil {
		return err
	}
	c.log.Info("publication submitted for review", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_id", astroConfig.ItemID, "external_url", result.PullRequest)
	return nil
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
	if _, err = tx.ExecContext(ctx, `UPDATE publications SET state=$2,external_id=$3,external_url=$4,error_message='',status_detail=$5,updated_at=now() WHERE id=$1`, item.ID, state, externalID, externalURL, raw); err != nil {
		return err
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

func (c *Coordinator) pollOne(ctx context.Context) error {
	var item publication
	var detail []byte
	err := c.db.QueryRowContext(ctx, `SELECT p.id::text,p.revision_id::text,r.id::text,r.owner_id::text,u.username,p.target,p.config,p.attempts,p.status_detail FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN resources r ON r.id=rr.resource_id JOIN users u ON u.id=r.owner_id WHERE p.target='astrobox' AND p.state='reviewing' AND p.updated_at<now()-interval '20 seconds' ORDER BY p.updated_at LIMIT 1`).Scan(&item.ID, &item.RevisionID, &item.ResourceID, &item.OwnerID, &item.OwnerName, &item.Target, &item.Config, &item.Attempts, &detail)
	if err != nil {
		return err
	}
	var statusDetail struct {
		PullRequestNumber int `json:"pull_request_number"`
	}
	if json.Unmarshal(detail, &statusDetail) != nil || statusDetail.PullRequestNumber <= 0 {
		return fmt.Errorf("publication %s has no pull request number", item.ID)
	}
	token, err := c.githubToken(ctx, item.OwnerID)
	if err != nil {
		return err
	}
	status, err := c.astro.PullRequest(ctx, token, statusDetail.PullRequestNumber)
	if err != nil {
		return err
	}
	if status.Merged {
		_, err = c.db.ExecContext(ctx, `UPDATE publications SET state='published',external_url=$2,error_message='',updated_at=now() WHERE id=$1`, item.ID, status.URL)
		if err == nil {
			c.log.Info("publication completed", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_url", status.URL)
		}
	} else if status.State == "closed" {
		_, err = c.db.ExecContext(ctx, `UPDATE publications SET state='failed',error_message='外部审核不通过',updated_at=now() WHERE id=$1`, item.ID)
		if err == nil {
			c.log.Warn("publication failed permanently", "publication_id", item.ID, "resource_id", item.ResourceID, "target", item.Target, "external_url", status.URL, "error", "external review rejected")
		}
	} else {
		_, err = c.db.ExecContext(ctx, `UPDATE publications SET updated_at=now() WHERE id=$1`, item.ID)
	}
	return err
}

func compactError(err error) string {
	message := strings.TrimSpace(err.Error())
	if len(message) > 1000 {
		return message[:1000]
	}
	return message
}
