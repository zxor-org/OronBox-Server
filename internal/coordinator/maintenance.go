package coordinator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func (c *Coordinator) maintain(ctx context.Context) error {
	_, _ = c.db.ExecContext(ctx, `DELETE FROM session_rotation_receipts WHERE expires_at<now()`)
	_, _ = c.db.ExecContext(ctx, `DELETE FROM creator_media_uploads u WHERE created_at<now()-interval '7 days' AND NOT EXISTS(SELECT 1 FROM resource_collection_revisions rr WHERE rr.icon_sha256=u.blob_sha256 OR rr.cover_sha256=u.blob_sha256)`)

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
AND NOT EXISTS(SELECT 1 FROM plugins WHERE package_sha256=b.sha256 OR icon_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM plugin_versions WHERE package_sha256=b.sha256 OR icon_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM blog_posts WHERE source_sha='' AND (cover_sha256=b.sha256 OR body LIKE '%'||b.sha256||'%'))
AND NOT EXISTS(SELECT 1 FROM resource_collection_revisions WHERE icon_sha256=b.sha256 OR cover_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM creator_media_uploads WHERE blob_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM home_banners WHERE cover_sha256=b.sha256)
ORDER BY b.created_at LIMIT 1 FOR UPDATE OF b SKIP LOCKED`, time.Now().UTC().Add(-c.cfg.Retention.OrphanBlobs)).Scan(&sha, &localKey, &r2Key)
		if errors.Is(err, sql.ErrNoRows) {
			tx.Rollback()
			return nil
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		// Let FK checks reject any live reference before deleting external files.
		result, err := tx.ExecContext(ctx, `DELETE FROM blobs b WHERE sha256=$1
AND NOT EXISTS(SELECT 1 FROM revision_media WHERE blob_sha256=$1)
AND NOT EXISTS(SELECT 1 FROM revision_artifacts WHERE blob_sha256=$1)
AND NOT EXISTS(SELECT 1 FROM plugins WHERE package_sha256=b.sha256 OR icon_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM plugin_versions WHERE package_sha256=b.sha256 OR icon_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM blog_posts WHERE source_sha='' AND (cover_sha256=b.sha256 OR body LIKE '%'||b.sha256||'%'))
AND NOT EXISTS(SELECT 1 FROM resource_collection_revisions WHERE icon_sha256=b.sha256 OR cover_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM creator_media_uploads WHERE blob_sha256=b.sha256)
AND NOT EXISTS(SELECT 1 FROM home_banners WHERE cover_sha256=b.sha256)`, sha)
		if err != nil {
			tx.Rollback()
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			tx.Rollback()
			continue
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

		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
