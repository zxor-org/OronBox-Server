package store

import "context"

type BlobRecord struct {
	SHA256    string
	Size      int64
	MediaType string
	LocalKey  string
	R2Key     string
	R2State   string
}

func (s *Store) Blob(ctx context.Context, sha256 string) (BlobRecord, error) {
	var blob BlobRecord
	err := s.db.QueryRowContext(ctx, `SELECT b.sha256,b.size_bytes,b.media_type,b.local_key,COALESCE(r.object_key,''),COALESCE(r.state,'') FROM blobs b LEFT JOIN blob_replicas r ON r.blob_sha256=b.sha256 AND r.backend='r2' WHERE b.sha256=$1`, sha256).
		Scan(&blob.SHA256, &blob.Size, &blob.MediaType, &blob.LocalKey, &blob.R2Key, &blob.R2State)
	return blob, err
}

// PublicBlob returns a blob only while it is referenced by the currently
// published revision of an active, visible resource. Rejected,
// superseded, archived and suspended content stays private.
func (s *Store) PublicBlob(ctx context.Context, sha256 string) (BlobRecord, error) {
	var blob BlobRecord
	err := s.db.QueryRowContext(ctx, `
SELECT blob.sha256,blob.size_bytes,blob.media_type,blob.local_key,
       COALESCE(replica.object_key,''),COALESCE(replica.state,'')
FROM blobs blob
LEFT JOIN blob_replicas replica
  ON replica.blob_sha256=blob.sha256 AND replica.backend='r2'
WHERE blob.sha256=$1
  AND EXISTS (
    SELECT 1
    FROM resources resource
    WHERE resource.state='active'
      AND resource.moderation_state='visible'
      AND resource.current_revision_id IS NOT NULL
      AND (
        EXISTS (
          SELECT 1
          FROM revision_artifacts artifact
          WHERE artifact.revision_id=resource.current_revision_id
            AND artifact.blob_sha256=blob.sha256
        )
        OR EXISTS (
          SELECT 1
          FROM revision_media media
          WHERE media.revision_id=resource.current_revision_id
            AND media.blob_sha256=blob.sha256
        )
      )
  )`, sha256).
		Scan(&blob.SHA256, &blob.Size, &blob.MediaType, &blob.LocalKey, &blob.R2Key, &blob.R2State)
	return blob, err
}

func (s *Store) SetBlobR2State(ctx context.Context, sha256, state, key string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO blob_replicas(blob_sha256,backend,state,object_key,updated_at) VALUES($1,'r2',$2,$3,now()) ON CONFLICT(blob_sha256,backend) DO UPDATE SET state=excluded.state,object_key=excluded.object_key,updated_at=now()`, sha256, state, key)
	return err
}
