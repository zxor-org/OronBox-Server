package creator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"strings"
)

type CollectionSubmission struct {
	Slug             string       `json:"slug"`
	Name             string       `json:"name"`
	Summary          string       `json:"summary"`
	Description      string       `json:"description"`
	Kind             ResourceKind `json:"kind"`
	ResourceIDs      []string     `json:"resource_ids"`
	RepresentativeID string       `json:"representative_resource_id"`
	Icon             string       `json:"icon_sha256"`
	Cover            string       `json:"cover_sha256"`
	ExpectedVersion  *int64       `json:"expected_edit_version"`
	ReplacePending   string       `json:"replace_pending_revision_id"`
}

func (s *Service) SubmitCollection(ctx context.Context, owner, id string, in CollectionSubmission) (Collection, error) {
	if !validCollectionText(in.Name, in.Summary) || len([]rune(in.Description)) > 100000 || len(in.ResourceIDs) == 0 || len(in.ResourceIDs) > 200 {
		return Collection{}, fmt.Errorf("%w: collection requires valid text and 1–200 members", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Collection{}, err
	}
	defer tx.Rollback()
	var kind ResourceKind
	var current, pending string
	var version int64
	if id == "" {
		if !slugPattern.MatchString(in.Slug) || !in.Kind.Valid() {
			return Collection{}, ErrInvalid
		}
		id = uuid.NewString()
		kind = in.Kind
		if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collections(id,owner_id,slug,kind)VALUES($1,$2,$3,$4)`, id, owner, in.Slug, kind); err != nil {
			return Collection{}, err
		}
	} else {
		err = tx.QueryRowContext(ctx, `SELECT kind,COALESCE(current_revision_id::text,''),edit_version FROM resource_collections WHERE id=$1 AND owner_id=$2 FOR UPDATE`, id, owner).Scan(&kind, &current, &version)
		if errors.Is(err, sql.ErrNoRows) {
			return Collection{}, ErrNotFound
		}
		if err != nil {
			return Collection{}, err
		}
		if in.ExpectedVersion == nil || *in.ExpectedVersion != version {
			return Collection{}, fmt.Errorf("%w: collection revision_conflict", ErrConflict)
		}
	}
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM resource_collection_revisions WHERE collection_id=$1 AND state='pending' ORDER BY revision_no DESC LIMIT 1`, id).Scan(&pending)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Collection{}, err
	}
	if pending != in.ReplacePending {
		return Collection{}, fmt.Errorf("%w: explicitly replace the current pending collection revision", ErrConflict)
	}
	seen := map[string]bool{}
	for _, rid := range in.ResourceIDs {
		if _, err = uuid.Parse(rid); err != nil || seen[rid] {
			return Collection{}, ErrInvalid
		}
		seen[rid] = true
	}
	if in.RepresentativeID == "" {
		in.RepresentativeID = in.ResourceIDs[0]
	}
	if !seen[in.RepresentativeID] {
		return Collection{}, fmt.Errorf("%w: representative must be a member", ErrInvalid)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text FROM resources WHERE id=ANY($1) AND owner_id=$2 AND kind=$3 AND moderation_state NOT IN ('deleted','frozen') AND (collection_id IS NULL OR collection_id=$4) ORDER BY id FOR UPDATE`, in.ResourceIDs, owner, kind, id)
	if err != nil {
		return Collection{}, err
	}
	count := 0
	for rows.Next() {
		count++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Collection{}, err
	}
	if count != len(in.ResourceIDs) {
		return Collection{}, fmt.Errorf("%w: invalid collection membership", ErrInvalid)
	}
	for _, sha := range []string{in.Icon, in.Cover} {
		if sha == "" {
			continue
		}
		var owned bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM creator_media_uploads WHERE owner_id=$1 AND blob_sha256=$2 UNION ALL SELECT 1 FROM revision_media m JOIN resource_revisions rr ON rr.id=m.revision_id JOIN resources r ON r.id=rr.resource_id WHERE r.owner_id=$1 AND m.blob_sha256=$2 UNION ALL SELECT 1 FROM resource_collection_revisions rr JOIN resource_collections c ON c.id=rr.collection_id WHERE c.owner_id=$1 AND (rr.icon_sha256=$2 OR rr.cover_sha256=$2))`, owner, sha).Scan(&owned); err != nil {
			return Collection{}, err
		}
		if !owned {
			return Collection{}, fmt.Errorf("%w: media ownership", ErrInvalid)
		}
	}
	if pending != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE resource_collection_revisions SET state='superseded',updated_at=now() WHERE id=$1`, pending); err != nil {
			return Collection{}, err
		}
	}
	revision := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary,description,icon_sha256,cover_sha256,representative_resource_id,created_by,base_revision_id) SELECT $1,$2,COALESCE(max(revision_no),0)+1,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9,NULLIF($10,'')::uuid FROM resource_collection_revisions WHERE collection_id=$2`, revision, id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Summary), in.Description, in.Icon, in.Cover, in.RepresentativeID, owner, current); err != nil {
		return Collection{}, err
	}
	for pos, rid := range in.ResourceIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collection_revision_members(id,revision_id,resource_id,resource_slug,resource_name,position) SELECT $1,$2,r.id,r.slug,COALESCE(rr.name,r.draft_name),$4 FROM resources r LEFT JOIN resource_revisions rr ON rr.id=r.current_revision_id WHERE r.id=$3`, uuid.NewString(), revision, rid, pos); err != nil {
			return Collection{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resource_collections SET edit_version=edit_version+1,updated_at=now() WHERE id=$1`, id); err != nil {
		return Collection{}, err
	}
	if err = tx.Commit(); err != nil {
		return Collection{}, err
	}
	return s.Collection(ctx, owner, id)
}
