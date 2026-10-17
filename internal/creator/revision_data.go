package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/blob"
)

func (s *Service) revisionLinks(ctx context.Context, revisionID string) ([]ResourceLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT title,url FROM revision_links WHERE revision_id=$1 ORDER BY position`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []ResourceLink
	for rows.Next() {
		var item ResourceLink
		if err := rows.Scan(&item.Title, &item.URL); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) revisionArtifacts(ctx context.Context, revisionID string, publicView bool) ([]Artifact, error) {
	// Workspace edits bindings by device UUID; the public store page displays
	// codenames.
	deviceExpr := "binding.device_id::text"
	if publicView {
		deviceExpr = "d.codename"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id::text,a.blob_sha256,a.original_name,a.package_format,a.package_id,a.package_version,a.analysis,COALESCE(b.size_bytes,0),COALESCE((SELECT jsonb_agg(`+deviceExpr+` ORDER BY `+deviceExpr+`) FROM revision_artifact_devices binding JOIN devices d ON d.id=binding.device_id WHERE binding.artifact_id=a.id),'[]') FROM revision_artifacts a LEFT JOIN blobs b ON b.sha256=a.blob_sha256 WHERE a.revision_id=$1 ORDER BY a.created_at`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Artifact
	for rows.Next() {
		var item Artifact
		var analysis, devices []byte
		if err := rows.Scan(&item.ID, &item.BlobSHA256, &item.OriginalName, &item.PackageFormat, &item.PackageID, &item.Version, &analysis, &item.SizeBytes, &devices); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(analysis, &item.Analysis)
		_ = json.Unmarshal(devices, &item.DeviceIDs)
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) revisionMedia(ctx context.Context, revisionID string) ([]Media, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id::text,m.blob_sha256,m.role,m.position,m.width,m.height,COALESCE(b.size_bytes,0) FROM revision_media m LEFT JOIN blobs b ON b.sha256=m.blob_sha256 WHERE m.revision_id=$1 ORDER BY m.role,m.position`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Media
	for rows.Next() {
		var item Media
		if err := rows.Scan(&item.ID, &item.BlobSHA256, &item.Role, &item.Position, &item.Width, &item.Height, &item.SizeBytes); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) publications(ctx context.Context, revisionID string) ([]Publication, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,revision_id::text,target,state,config,external_id,external_url,error_message,status_detail,updated_at FROM publications WHERE revision_id=$1 ORDER BY target`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Publication
	for rows.Next() {
		var item Publication
		var config, detail []byte
		if err := rows.Scan(&item.ID, &item.RevisionID, &item.Target, &item.State, &config, &item.ExternalID, &item.ExternalURL, &item.ErrorMessage, &detail, &item.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(config, &item.Config)
		if len(detail) > 0 && string(detail) != "{}" {
			_ = json.Unmarshal(detail, &item.StatusDetail)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func event(ctx context.Context, tx *sql.Tx, resourceID, actorID, eventType string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO resource_events(resource_id,actor_id,event_type,payload) VALUES($1,NULLIF($2,'')::uuid,$3,$4)`, resourceID, actorID, eventType, encoded)
	return err
}

func unique(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

// OpenBlob serves a revision asset to its resource's owner, covering revisions
// that are still under review and therefore not publicly readable.
func (s *Service) OpenBlob(ctx context.Context, ownerID, resourceID, digest string) (blob.ReadSeekCloser, string, error) {
	var localKey, mediaType string
	err := s.db.QueryRowContext(ctx, `SELECT b.local_key,b.media_type FROM blobs b WHERE b.sha256=$1 AND (EXISTS(SELECT 1 FROM revision_media m JOIN resource_revisions rr ON rr.id=m.revision_id JOIN resources r ON r.id=rr.resource_id WHERE m.blob_sha256=b.sha256 AND r.id=$2 AND r.owner_id=$3) OR EXISTS(SELECT 1 FROM revision_artifacts a JOIN resource_revisions rr ON rr.id=a.revision_id JOIN resources r ON r.id=rr.resource_id WHERE a.blob_sha256=b.sha256 AND r.id=$2 AND r.owner_id=$3))`, digest, resourceID, ownerID).Scan(&localKey, &mediaType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	reader, err := s.blobs.Open(ctx, localKey)
	if err != nil {
		return nil, "", ErrNotFound
	}
	return reader, mediaType, nil
}
