package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type AdminRevisionGovernance struct {
	AuthorName         string   `json:"author_name"`
	SourceURL          string   `json:"source_url"`
	LicenseName        string   `json:"license_name"`
	AuthorizationNote  string   `json:"authorization_note"`
	CollectionID       string   `json:"collection_id"`
	CollectionPosition int      `json:"collection_position"`
	CollaboratorIDs    []string `json:"collaborator_ids"`
}

func (s *Store) AdminRevisionGovernance(ctx context.Context, revisionID string) (AdminRevisionGovernance, error) {
	var result AdminRevisionGovernance
	var source, collaborators []byte
	err := s.db.QueryRowContext(ctx, `SELECT governance_source,COALESCE(governance_collection_id::text,''),governance_collection_position,COALESCE((SELECT jsonb_agg(user_id::text ORDER BY user_id::text) FROM resource_revision_collaborators WHERE revision_id=$1),'[]'::jsonb) FROM resource_revisions WHERE id=$1`, revisionID).Scan(&source, &result.CollectionID, &result.CollectionPosition, &collaborators)
	if err != nil {
		return result, err
	}
	_ = json.Unmarshal(source, &result)
	_ = json.Unmarshal(collaborators, &result.CollaboratorIDs)
	return result, nil
}

func (s *Store) AdminSaveRevisionGovernance(ctx context.Context, resourceID, revisionID string, input AdminRevisionGovernance) error {
	input.AuthorName = strings.TrimSpace(input.AuthorName)
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	input.LicenseName = strings.TrimSpace(input.LicenseName)
	input.AuthorizationNote = strings.TrimSpace(input.AuthorizationNote)
	input.CollectionID = strings.TrimSpace(input.CollectionID)
	if len(input.AuthorName) > 120 || len(input.SourceURL) > 2048 || len(input.LicenseName) > 120 || len(input.AuthorizationNote) > 4000 || input.CollectionPosition < 0 {
		return fmt.Errorf("%w: governance fields", ErrAdminResourceConflict)
	}
	if input.SourceURL != "" {
		parsed, err := url.ParseRequestURI(input.SourceURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("%w: source URL", ErrAdminResourceConflict)
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	if input.CollectionID != "" {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resource_collections collection JOIN resources resource ON resource.id=$2 WHERE collection.id=$1 AND collection.owner_id=resource.owner_id AND collection.kind=resource.kind)`, input.CollectionID, resourceID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: incompatible collection", ErrAdminResourceConflict)
		}
	}
	source, _ := json.Marshal(map[string]string{"author_name": input.AuthorName, "source_url": input.SourceURL, "license_name": input.LicenseName, "authorization_note": input.AuthorizationNote})
	if _, err := tx.ExecContext(ctx, `UPDATE resource_revisions SET governance_source=$2,governance_collection_id=NULLIF($3,'')::uuid,governance_collection_position=$4 WHERE id=$1`, revisionID, source, input.CollectionID, input.CollectionPosition); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_revision_collaborators WHERE revision_id=$1`, revisionID); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range input.CollaboratorIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		result, err := tx.ExecContext(ctx, `INSERT INTO resource_revision_collaborators(revision_id,user_id) SELECT $1,id FROM users WHERE id=$2 AND id<>(SELECT owner_id FROM resources WHERE id=$3)`, revisionID, id, resourceID)
		if err != nil {
			return err
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return fmt.Errorf("%w: collaborator %s", ErrAdminResourceConflict, id)
		}
	}
	return tx.Commit()
}
