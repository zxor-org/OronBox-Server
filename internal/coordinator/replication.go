package coordinator

import (
	"context"

	"github.com/zxor-org/OronBox-Server/internal/blob"
)

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
