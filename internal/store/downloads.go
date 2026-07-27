package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrDownloadQuota = errors.New("daily download quota exceeded")

func (s *Store) RecordDownload(ctx context.Context, sha256, userID, ipHash string, dailyLimit int) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	identity := userID
	if identity == "" {
		identity = ipHash
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "download:"+identity); err != nil {
		return err
	}
	var resourceID, artifactID string
	err = tx.QueryRowContext(ctx, `
SELECT resource.id::text, artifact.id::text
FROM revision_artifacts artifact
JOIN resources resource ON resource.current_revision_id = artifact.revision_id
WHERE artifact.blob_sha256=$1 AND resource.moderation_state='visible'
LIMIT 1`, sha256).Scan(&resourceID, &artifactID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if userID != "" && dailyLimit > 0 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM download_events WHERE user_id=$1 AND created_at>now()-interval '24 hours'`, userID).Scan(&count); err != nil {
			return err
		}
		if count >= dailyLimit {
			return ErrDownloadQuota
		}
	}
	var duplicate bool
	if userID != "" {
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM download_events WHERE resource_id=$1 AND user_id=$2 AND created_at>now()-interval '24 hours')`, resourceID, userID).Scan(&duplicate)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM download_events WHERE resource_id=$1 AND user_id IS NULL AND ip_hash=$2 AND created_at>now()-interval '24 hours')`, resourceID, ipHash).Scan(&duplicate)
	}
	if err != nil {
		return err
	}
	if duplicate {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO download_events(resource_id,artifact_id,user_id,ip_hash) VALUES($1,$2,NULLIF($3,'')::uuid,$4)`, resourceID, artifactID, userID, ipHash); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resources SET download_count=download_count+1 WHERE id=$1`, resourceID); err != nil {
		return err
	}
	return tx.Commit()
}
