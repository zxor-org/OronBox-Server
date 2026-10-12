package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/observability"
)

func (a *application) runCoordinator(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	runOnce := func() {
		if a.db == nil || a.db.Pool == nil {
			return
		}

		// 1. Mark Phase: Reconcile active references across all resource/revision/collection/plugin tables
		markQuery := `
			WITH active_refs AS (
				SELECT icon_sha256 AS sha FROM resources WHERE icon_sha256 <> ''
				UNION ALL
				SELECT cover_sha256 AS sha FROM resources WHERE cover_sha256 <> ''
				UNION ALL
				SELECT jsonb_array_elements_text(preview_shas) AS sha 
				FROM resources 
				WHERE jsonb_typeof(preview_shas) = 'array'
				UNION ALL
				SELECT COALESCE(value->>'blob_sha256', value->>'sha256') AS sha 
				FROM resource_revisions, jsonb_each(downloads) 
				WHERE jsonb_typeof(downloads) = 'object' 
				  AND COALESCE(value->>'blob_sha256', value->>'sha256') IS NOT NULL 
				  AND length(COALESCE(value->>'blob_sha256', value->>'sha256')) = 64
				UNION ALL
				SELECT config->'draft_metadata'->>'icon_sha256' AS sha FROM resource_revisions
				UNION ALL
				SELECT config->'draft_metadata'->>'cover_sha256' AS sha FROM resource_revisions
				UNION ALL
				SELECT jsonb_array_elements_text(CASE WHEN jsonb_typeof(config->'draft_metadata'->'preview_shas') = 'array' THEN config->'draft_metadata'->'preview_shas' ELSE '[]'::jsonb END) AS sha FROM resource_revisions
				UNION ALL
				SELECT icon_sha256 AS sha FROM resource_collection_revisions WHERE icon_sha256 <> ''
				UNION ALL
				SELECT cover_sha256 AS sha FROM resource_collection_revisions WHERE cover_sha256 <> ''
				UNION ALL
				SELECT package_sha256 AS sha FROM plugin_versions WHERE package_sha256 <> ''
			),
			ref_counts AS (
				SELECT sha, COUNT(*)::int AS cnt
				FROM active_refs
				WHERE sha IS NOT NULL AND length(sha) = 64
				GROUP BY sha
			)
			UPDATE blobs b
			SET ref_count = COALESCE(rc.cnt, 0)
			FROM (
				SELECT b2.sha256, rc2.cnt
				FROM blobs b2
				LEFT JOIN ref_counts rc2 ON b2.sha256 = rc2.sha
			) rc
			WHERE b.sha256 = rc.sha256
		`
		if _, err := a.db.Pool.Exec(ctx, markQuery); err != nil {
			a.logger.Error("coordinator mark ref_count failed", "error", observability.SafeError(err))
			return // Never sweep using stale reference counts.
		}

		// 2. Sweep Phase: Find orphaned blobs created > 24 hours ago
		blobDir := a.cfg.BlobStorageDir
		if blobDir == "" {
			blobDir = "/var/lib/oronbox/blobs"
		}

		rows, err := a.db.Pool.Query(ctx, `
			DELETE FROM blobs 
			WHERE ref_count <= 0 AND created_at < now() - interval '24 hours'
			RETURNING sha256
		`)
		if err != nil {
			a.logger.Error("coordinator sweep query failed", "error", observability.SafeError(err))
		} else {
			var deletedSHAs []string
			for rows.Next() {
				var sha string
				if err := rows.Scan(&sha); err == nil {
					deletedSHAs = append(deletedSHAs, sha)
				}
			}
			rows.Close()

			for _, sha := range deletedSHAs {
				path := filepath.Join(blobDir, sha[:2], sha[2:4], sha)
				_ = os.Remove(path)
				altPath := filepath.Join(blobDir, sha)
				_ = os.Remove(altPath)

				if a.r2 != nil && a.r2.IsConfigured() {
					objKey := fmt.Sprintf("%s/%s/%s", sha[:2], sha[2:4], sha)
					_ = a.r2.DeleteObject(ctx, objKey)
				}
			}
		}

		// 3. R2 Replication Sync: Retry pending/failed R2 replicas
		if a.r2 != nil && a.r2.IsConfigured() {
			repRows, err := a.db.Pool.Query(ctx, `
				SELECT b.sha256, b.media_type, b.size_bytes, br.object_key
				FROM blob_replicas br
				JOIN blobs b ON b.sha256 = br.blob_sha256
				WHERE br.backend = 'r2' AND br.state IN ('pending', 'failed') AND br.attempts < 5
				LIMIT 20
			`)
			if err != nil {
				a.logger.Error("coordinator r2 sync query failed", "error", observability.SafeError(err))
			} else {
				type pendingRep struct {
					sha   string
					mtype string
					size  int64
					key   string
				}
				var pending []pendingRep
				for repRows.Next() {
					var p pendingRep
					if err := repRows.Scan(&p.sha, &p.mtype, &p.size, &p.key); err == nil {
						pending = append(pending, p)
					}
				}
				repRows.Close()

				for _, p := range pending {
					localPath := filepath.Join(blobDir, p.sha[:2], p.sha[2:4], p.sha)
					if _, err := os.Stat(localPath); os.IsNotExist(err) {
						localPath = filepath.Join(blobDir, p.sha)
					}
					f, err := os.Open(localPath)
					if err != nil {
						_, _ = a.db.Pool.Exec(ctx, `
							UPDATE blob_replicas
							SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now()
							WHERE blob_sha256 = $1 AND backend = 'r2'
						`, p.sha, err.Error())
						continue
					}
					err = a.r2.PutObject(ctx, p.key, p.mtype, f, p.size, p.sha)
					f.Close()
					if err != nil {
						_, _ = a.db.Pool.Exec(ctx, `
							UPDATE blob_replicas
							SET state = 'failed', error_message = $2, attempts = attempts + 1, updated_at = now()
							WHERE blob_sha256 = $1 AND backend = 'r2'
						`, p.sha, err.Error())
					} else {
						_, _ = a.db.Pool.Exec(ctx, `
							UPDATE blob_replicas
							SET state = 'ready', error_message = '', updated_at = now()
							WHERE blob_sha256 = $1 AND backend = 'r2'
						`, p.sha)
					}
				}
			}
		}

	}

	runOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runOnce()
		}
	}
}
