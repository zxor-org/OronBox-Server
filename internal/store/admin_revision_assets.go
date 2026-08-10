package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type AdminMediaInput struct {
	SHA256   string
	Role     string
	Position int
	Width    int
	Height   int
}

type AdminArtifactInput struct {
	SHA256        string
	OriginalName  string
	PackageFormat string
	PackageID     string
	Version       string
	Analysis      any
	DeviceIDs     []string
}

func lockAdminDraft(ctx context.Context, tx *sql.Tx, resourceID, revisionID string) error {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT true FROM resource_revisions revision WHERE revision.id=$1 AND revision.resource_id=$2 AND ((revision.state='draft' AND revision.created_via='admin') OR (revision.state='submitted' AND EXISTS(SELECT 1 FROM review_cases review WHERE review.revision_id=revision.id AND review.state='pending'))) FOR UPDATE`, revisionID, resourceID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: editable draft or pending review was not found", ErrAdminResourceConflict)
	}
	return err
}

func (s *Store) AdminAddRevisionMedia(ctx context.Context, resourceID, revisionID string, input AdminMediaInput) error {
	if _, err := uuid.Parse(resourceID); err != nil {
		return ErrAdminResourceNotFound
	}
	if _, err := uuid.Parse(revisionID); err != nil {
		return ErrAdminResourceNotFound
	}
	input.SHA256 = strings.ToLower(strings.TrimSpace(input.SHA256))
	input.Role = strings.TrimSpace(input.Role)
	if len(input.SHA256) != 64 || (input.Role != "preview" && input.Role != "icon" && input.Role != "cover") || input.Width < 1 || input.Height < 1 || input.Width > 1500 || input.Height > 1500 {
		return fmt.Errorf("%w: invalid media", ErrAdminResourceConflict)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	if input.Role == "preview" {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(position),-1)+1 FROM revision_media WHERE revision_id=$1 AND role='preview'`, revisionID).Scan(&input.Position); err != nil {
			return err
		}
	} else {
		input.Position = 0
		if _, err := tx.ExecContext(ctx, `DELETE FROM revision_media WHERE revision_id=$1 AND role=$2`, revisionID, input.Role); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO revision_media(id,revision_id,blob_sha256,role,position,width,height) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), revisionID, input.SHA256, input.Role, input.Position, input.Width, input.Height); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AdminDeleteRevisionMedia(ctx context.Context, resourceID, revisionID, mediaID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM revision_media WHERE id=$1 AND revision_id=$2`, mediaID, revisionID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrAdminResourceNotFound
	}
	if _, err := tx.ExecContext(ctx, `WITH ordered AS (SELECT id,row_number() OVER (PARTITION BY role ORDER BY position,id)-1 position FROM revision_media WHERE revision_id=$1) UPDATE revision_media media SET position=ordered.position FROM ordered WHERE media.id=ordered.id`, revisionID); err != nil {
		return err
	}
	return tx.Commit()
}

// AdminMoveRevisionMedia moves one image within its role. The complete role is
// rewritten to contiguous zero-based positions while the draft row is locked.
func (s *Store) AdminMoveRevisionMedia(ctx context.Context, resourceID, revisionID, mediaID string, position int) error {
	if position < 0 {
		return fmt.Errorf("%w: invalid media position", ErrAdminResourceConflict)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRowContext(ctx, `SELECT role FROM revision_media WHERE id=$1 AND revision_id=$2 FOR UPDATE`, mediaID, revisionID).Scan(&role); errors.Is(err, sql.ErrNoRows) {
		return ErrAdminResourceNotFound
	} else if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM revision_media WHERE revision_id=$1 AND role=$2 ORDER BY position,id FOR UPDATE`, revisionID, role)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if position >= len(ids) {
		return fmt.Errorf("%w: invalid media position", ErrAdminResourceConflict)
	}
	from := -1
	for i, id := range ids {
		if id == mediaID {
			from = i
			break
		}
	}
	if from < 0 {
		return ErrAdminResourceNotFound
	}
	ids = append(ids[:from], ids[from+1:]...)
	ids = append(ids, "")
	copy(ids[position+1:], ids[position:])
	ids[position] = mediaID
	if _, err := tx.ExecContext(ctx, `UPDATE revision_media SET position=-(position+1) WHERE revision_id=$1 AND role=$2`, revisionID, role); err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE revision_media SET position=$2 WHERE id=$1`, id, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AdminAddRevisionArtifact(ctx context.Context, resourceID, revisionID string, input AdminArtifactInput) error {
	input.OriginalName = strings.TrimSpace(input.OriginalName)
	if input.OriginalName == "" || len(input.SHA256) != 64 || len(input.DeviceIDs) == 0 {
		return fmt.Errorf("%w: invalid resource file", ErrAdminResourceConflict)
	}
	analysis, err := json.Marshal(input.Analysis)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	var valid int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM devices WHERE enabled AND id=ANY($1)`, input.DeviceIDs).Scan(&valid); err != nil {
		return err
	}
	if valid != len(input.DeviceIDs) {
		return fmt.Errorf("%w: unknown or disabled device", ErrAdminResourceConflict)
	}
	artifactID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO revision_artifacts(id,revision_id,blob_sha256,original_name,package_format,package_id,package_version,analysis) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, artifactID, revisionID, input.SHA256, input.OriginalName, input.PackageFormat, input.PackageID, input.Version, analysis); err != nil {
		return err
	}
	for _, deviceID := range input.DeviceIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO revision_artifact_devices(revision_id,artifact_id,device_id) VALUES($1,$2,$3)`, revisionID, artifactID, deviceID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) AdminDeleteRevisionArtifact(ctx context.Context, resourceID, revisionID, artifactID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM revision_artifacts WHERE id=$1 AND revision_id=$2`, artifactID, revisionID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrAdminResourceNotFound
	}
	return tx.Commit()
}

func (s *Store) AdminSetArtifactDevices(ctx context.Context, resourceID, revisionID, artifactID string, deviceIDs []string) error {
	if len(deviceIDs) == 0 {
		return fmt.Errorf("%w: at least one device is required", ErrAdminResourceConflict)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	var blobSHA, originalName, packageFormat, packageID, packageVersion string
	if err := tx.QueryRowContext(ctx, `SELECT blob_sha256,original_name,package_format,package_id,package_version FROM revision_artifacts WHERE id=$1 AND revision_id=$2 FOR UPDATE`, artifactID, revisionID).Scan(&blobSHA, &originalName, &packageFormat, &packageID, &packageVersion); errors.Is(err, sql.ErrNoRows) {
		return ErrAdminResourceNotFound
	} else if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM revision_artifacts WHERE revision_id=$1 AND blob_sha256=$2 AND original_name=$3 AND package_format=$4 AND package_id=$5 AND package_version=$6 FOR UPDATE`, revisionID, blobSHA, originalName, packageFormat, packageID, packageVersion)
	if err != nil {
		return err
	}
	artifactIDs := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		artifactIDs = append(artifactIDs, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM revision_artifact_devices WHERE artifact_id=ANY($1::uuid[])`, artifactIDs); err != nil {
		return err
	}
	for _, duplicateID := range artifactIDs {
		for _, id := range deviceIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO revision_artifact_devices(revision_id,artifact_id,device_id) SELECT $1,$2,id FROM devices WHERE id=$3 AND enabled`, revisionID, duplicateID, id); err != nil {
				return err
			}
		}
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM revision_artifact_devices WHERE artifact_id=ANY($1::uuid[])`, artifactIDs).Scan(&count); err != nil {
		return err
	}
	if count != len(deviceIDs)*len(artifactIDs) {
		return fmt.Errorf("%w: unknown or disabled device", ErrAdminResourceConflict)
	}
	return tx.Commit()
}
