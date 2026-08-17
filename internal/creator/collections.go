package creator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

func validCollectionText(name, summary string) bool {
	name = strings.TrimSpace(name)
	return name != "" && len([]rune(name)) <= 120 && len([]rune(summary)) <= 1000 &&
		strings.IndexFunc(name+summary, unicode.IsControl) < 0
}

// CreateCollection creates a hidden collection and its first metadata review.
// Resources remain independently discoverable until the collection is approved.
func (s *Service) CreateCollection(ctx context.Context, ownerID, slug, name, summary string, kind ResourceKind) (Collection, error) {
	slug, name, summary = strings.ToLower(strings.TrimSpace(slug)), strings.TrimSpace(name), strings.TrimSpace(summary)
	if !slugPattern.MatchString(slug) || !kind.Valid() || !validCollectionText(name, summary) {
		return Collection{}, fmt.Errorf("%w: collection slug, name, summary, or kind", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Collection{}, err
	}
	defer tx.Rollback()
	collectionID, revisionID := uuid.NewString(), uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collections(id,owner_id,slug,kind) VALUES($1,$2,$3,$4)`, collectionID, ownerID, slug, kind); err != nil {
		return Collection{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary,created_by) VALUES($1,$2,1,$3,$4,$5)`, revisionID, collectionID, name, summary, ownerID); err != nil {
		return Collection{}, err
	}
	if err = tx.Commit(); err != nil {
		return Collection{}, err
	}
	return s.Collection(ctx, ownerID, collectionID)
}

// UpdateCollectionMetadata submits a new metadata revision while retaining the
// last approved public revision until reviewers decide it.
func (s *Service) UpdateCollectionMetadata(ctx context.Context, ownerID, collectionID, name, summary string) (Collection, error) {
	name, summary = strings.TrimSpace(name), strings.TrimSpace(summary)
	if !validCollectionText(name, summary) {
		return Collection{}, fmt.Errorf("%w: collection name or summary", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Collection{}, err
	}
	defer tx.Rollback()
	var next int
	var lockedID, baseID, pendingID string
	var enabled bool
	var representativeID sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT collection.id::text,COALESCE(collection.current_revision_id::text,''),COALESCE(pending.id::text,''),COALESCE(pending.enabled,collection.enabled),COALESCE(pending.representative_resource_id,collection.representative_resource_id)::text FROM resource_collections collection LEFT JOIN LATERAL (SELECT id,enabled,representative_resource_id FROM resource_collection_revisions WHERE collection_id=collection.id AND state='pending' ORDER BY revision_no DESC LIMIT 1) pending ON true WHERE collection.id=$1 AND collection.owner_id=$2 FOR UPDATE OF collection`, collectionID, ownerID).Scan(&lockedID, &baseID, &pendingID, &enabled, &representativeID); errors.Is(err, sql.ErrNoRows) {
		return Collection{}, ErrNotFound
	} else if err != nil {
		return Collection{}, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM resource_collection_revisions WHERE collection_id=$1`, collectionID).Scan(&next); err != nil {
		return Collection{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resource_collection_revisions SET state='superseded',updated_at=now() WHERE collection_id=$1 AND state='pending'`, collectionID); err != nil {
		return Collection{}, err
	}
	revisionID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary,enabled,representative_resource_id,created_by,base_revision_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9,'')::uuid)`, revisionID, collectionID, next, name, summary, enabled, representativeID, ownerID, baseID); err != nil {
		return Collection{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collection_revision_members(id,revision_id,resource_id,resource_slug,resource_name,position) SELECT md5(random()::text||clock_timestamp()::text||snapshot.id::text)::uuid,$2,snapshot.resource_id,snapshot.resource_slug,snapshot.resource_name,snapshot.position FROM resource_collection_revision_members snapshot WHERE snapshot.revision_id=NULLIF($3,'')::uuid UNION ALL SELECT md5(random()::text||clock_timestamp()::text||resource.id::text)::uuid,$2,resource.id,resource.slug,COALESCE(current.name,resource.draft_name),row_number() OVER (ORDER BY resource.collection_position,resource.created_at,resource.id)-1 FROM resources resource LEFT JOIN resource_revisions current ON current.id=resource.current_revision_id WHERE resource.collection_id=$1 AND $3=''`, collectionID, revisionID, pendingID); err != nil {
		return Collection{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resource_collections SET updated_at=now() WHERE id=$1`, collectionID); err != nil {
		return Collection{}, err
	}
	if err = tx.Commit(); err != nil {
		return Collection{}, err
	}
	return s.Collection(ctx, ownerID, collectionID)
}

func scanCollection(scanner interface{ Scan(...any) error }) (Collection, error) {
	var item Collection
	var currentID, representativeID sql.NullString
	err := scanner.Scan(&item.ID, &item.OwnerID, &item.Slug, &item.Platform, &item.Kind, &currentID, &representativeID, &item.Enabled, &item.ResourceCount, &item.TotalCoins, &item.CreatedAt, &item.UpdatedAt)
	if currentID.Valid {
		item.CurrentRevisionID = currentID.String
	}
	if representativeID.Valid {
		item.RepresentativeResourceID = representativeID.String
	}
	return item, err
}

const collectionSelect = `SELECT c.id::text,c.owner_id::text,c.slug,c.platform,c.kind,c.current_revision_id::text,c.representative_resource_id::text,c.enabled,
(SELECT count(*) FROM resources r WHERE r.collection_id=c.id),
COALESCE((SELECT sum(v.coins) FROM resources r JOIN resource_coin_votes v ON v.resource_id=r.id AND v.invalidated_at IS NULL WHERE r.collection_id=c.id),0),c.created_at,c.updated_at`

func (s *Service) Collection(ctx context.Context, ownerID, collectionID string) (Collection, error) {
	item, err := scanCollection(s.db.QueryRowContext(ctx, collectionSelect+` FROM resource_collections c WHERE c.id=$1 AND c.owner_id=$2`, collectionID, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return Collection{}, ErrNotFound
	}
	if err != nil {
		return Collection{}, err
	}
	if item.CurrentRevisionID != "" {
		revision, err := s.collectionRevision(ctx, item.CurrentRevisionID)
		if err != nil {
			return Collection{}, err
		}
		item.CurrentRevision = &revision
	}
	var pendingID string
	err = s.db.QueryRowContext(ctx, `SELECT id::text FROM resource_collection_revisions WHERE collection_id=$1 AND state='pending' ORDER BY revision_no DESC LIMIT 1`, item.ID).Scan(&pendingID)
	if err == nil {
		revision, revisionErr := s.collectionRevision(ctx, pendingID)
		if revisionErr != nil {
			return Collection{}, revisionErr
		}
		item.PendingRevision = &revision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Collection{}, err
	}
	return item, nil
}

func (s *Service) collectionRevision(ctx context.Context, revisionID string) (CollectionRevision, error) {
	var item CollectionRevision
	err := s.db.QueryRowContext(ctx, `SELECT id::text,collection_id::text,revision_no,name,summary,state,review_note,enabled,COALESCE(representative_resource_id::text,''),created_via,COALESCE(base_revision_id::text,''),created_at,updated_at FROM resource_collection_revisions WHERE id=$1`, revisionID).
		Scan(&item.ID, &item.CollectionID, &item.Number, &item.Name, &item.Summary, &item.State, &item.ReviewNote, &item.Enabled, &item.RepresentativeResourceID, &item.CreatedVia, &item.BaseRevisionID, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		rows, rowsErr := s.db.QueryContext(ctx, `SELECT COALESCE(resource_id::text,'') FROM resource_collection_revision_members WHERE revision_id=$1 ORDER BY position`, revisionID)
		if rowsErr != nil {
			return item, rowsErr
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				return item, scanErr
			}
			if id != "" {
				item.ResourceIDs = append(item.ResourceIDs, id)
			}
		}
		err = rows.Err()
	}
	return item, err
}

func (s *Service) ListCollections(ctx context.Context, ownerID string) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM resource_collections WHERE owner_id=$1 ORDER BY updated_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	items := make([]Collection, 0, len(ids))
	for _, id := range ids {
		item, err := s.Collection(ctx, ownerID, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) SetCollectionResources(ctx context.Context, ownerID, collectionID, representativeID string, resourceIDs []string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var kind, baseID, name, summary string
	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT collection.kind,COALESCE(collection.current_revision_id::text,''),collection.enabled,COALESCE(pending.name,current.name,collection.slug),COALESCE(pending.summary,current.summary,'') FROM resource_collections collection LEFT JOIN resource_collection_revisions current ON current.id=collection.current_revision_id LEFT JOIN LATERAL (SELECT name,summary FROM resource_collection_revisions WHERE collection_id=collection.id AND state='pending' ORDER BY revision_no DESC LIMIT 1) pending ON true WHERE collection.id=$1 AND collection.owner_id=$2 FOR UPDATE OF collection`, collectionID, ownerID).Scan(&kind, &baseID, &enabled, &name, &summary); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	seen := make(map[string]bool, len(resourceIDs))
	for _, resourceID := range resourceIDs {
		if seen[resourceID] {
			return fmt.Errorf("%w: duplicate collection resource", ErrInvalid)
		}
		seen[resourceID] = true
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resources WHERE id=$1 AND owner_id=$2 AND kind=$3 AND (collection_id IS NULL OR collection_id=$4))`, resourceID, ownerID, kind, collectionID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return fmt.Errorf("%w: collection resource", ErrInvalid)
		}
	}
	if representativeID == "" && len(resourceIDs) > 0 {
		representativeID = resourceIDs[0]
	}
	if representativeID != "" && !seen[representativeID] {
		return fmt.Errorf("%w: representative must belong to collection", ErrInvalid)
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM resource_collection_revisions WHERE collection_id=$1`, collectionID).Scan(&next); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_collection_revisions SET state='superseded',updated_at=now() WHERE collection_id=$1 AND state='pending'`, collectionID); err != nil {
		return err
	}
	revisionID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary,enabled,representative_resource_id,created_by,base_revision_id) VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,$8,NULLIF($9,'')::uuid)`, revisionID, collectionID, next, name, summary, enabled, representativeID, ownerID, baseID); err != nil {
		return err
	}
	for position, resourceID := range resourceIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_collection_revision_members(id,revision_id,resource_id,resource_slug,resource_name,position) SELECT $1,$2,resource.id,resource.slug,COALESCE(current.name,resource.draft_name),$4 FROM resources resource LEFT JOIN resource_revisions current ON current.id=resource.current_revision_id WHERE resource.id=$3`, uuid.NewString(), revisionID, resourceID, position); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_collections SET updated_at=now() WHERE id=$1`, collectionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) DeleteCollection(ctx context.Context, ownerID, collectionID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var baseID, pendingID, name, summary string
	var representative sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(collection.current_revision_id::text,''),COALESCE(pending.id::text,''),COALESCE(pending.name,current.name,collection.slug),COALESCE(pending.summary,current.summary,''),COALESCE(pending.representative_resource_id,collection.representative_resource_id)::text FROM resource_collections collection LEFT JOIN resource_collection_revisions current ON current.id=collection.current_revision_id LEFT JOIN LATERAL (SELECT id,name,summary,representative_resource_id FROM resource_collection_revisions WHERE collection_id=collection.id AND state='pending' ORDER BY revision_no DESC LIMIT 1) pending ON true WHERE collection.id=$1 AND collection.owner_id=$2 FOR UPDATE OF collection`, collectionID, ownerID).Scan(&baseID, &pendingID, &name, &summary, &representative); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM resource_collection_revisions WHERE collection_id=$1`, collectionID).Scan(&next); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_collection_revisions SET state='superseded',updated_at=now() WHERE collection_id=$1 AND state='pending'`, collectionID); err != nil {
		return err
	}
	revisionID := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary,enabled,representative_resource_id,created_by,base_revision_id) VALUES($1,$2,$3,$4,$5,false,$6,$7,NULLIF($8,'')::uuid)`, revisionID, collectionID, next, name, summary, representative, ownerID, baseID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_collection_revision_members(id,revision_id,resource_id,resource_slug,resource_name,position) SELECT md5(random()::text||clock_timestamp()::text||snapshot.id::text)::uuid,$2::uuid,snapshot.resource_id,snapshot.resource_slug,snapshot.resource_name,snapshot.position FROM resource_collection_revision_members snapshot WHERE snapshot.revision_id=NULLIF($3,'')::uuid UNION ALL SELECT md5(random()::text||clock_timestamp()::text||resource.id::text)::uuid,$2::uuid,resource.id,resource.slug,COALESCE(current.name,resource.draft_name),row_number() OVER (ORDER BY resource.collection_position,resource.created_at,resource.id)-1 FROM resources resource LEFT JOIN resource_revisions current ON current.id=resource.current_revision_id WHERE resource.collection_id=$1 AND $3=''`, collectionID, revisionID, pendingID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_collections SET updated_at=now() WHERE id=$1`, collectionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ReviewCollection(ctx context.Context, revisionID, reviewerID string, approve bool, note string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state := "rejected"
	if approve {
		state = "approved"
	}
	var collectionID string
	err = tx.QueryRowContext(ctx, `UPDATE resource_collection_revisions SET state=$2,reviewer_id=NULLIF($3,'')::uuid,review_note=$4,updated_at=now() WHERE id=$1 AND state='pending' RETURNING collection_id::text`, revisionID, state, reviewerID, strings.TrimSpace(note)).Scan(&collectionID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if approve {
		if _, err = tx.ExecContext(ctx, `UPDATE resource_collection_revisions SET state='superseded',updated_at=now() WHERE collection_id=$1 AND state='approved' AND id<>$2`, collectionID, revisionID); err != nil {
			return err
		}
		var representativeID sql.NullString
		var enabled bool
		if err := tx.QueryRowContext(ctx, `SELECT representative_resource_id::text,enabled FROM resource_collection_revisions WHERE id=$1`, revisionID).Scan(&representativeID, &enabled); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resources resource SET collection_id=NULL,collection_position=0,updated_at=now() WHERE resource.collection_id=$1 AND NOT EXISTS(SELECT 1 FROM resource_collection_revision_members snapshot WHERE snapshot.revision_id=$2 AND snapshot.resource_id=resource.id)`, collectionID, revisionID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE resources resource SET collection_id=$1,collection_position=snapshot.position,updated_at=now() FROM resource_collection_revision_members snapshot JOIN resource_collections collection ON collection.id=$1 WHERE snapshot.revision_id=$2 AND snapshot.resource_id=resource.id AND resource.owner_id=collection.owner_id AND resource.kind=collection.kind AND (resource.collection_id IS NULL OR resource.collection_id=$1)`, collectionID, revisionID)
		if err != nil {
			return err
		}
		var expected int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM resource_collection_revision_members WHERE revision_id=$1`, revisionID).Scan(&expected); err != nil {
			return err
		}
		if changed, _ := result.RowsAffected(); int(changed) != expected {
			return fmt.Errorf("%w: collection membership changed since submission", ErrConflict)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resource_collections SET current_revision_id=$2,published_at=now(),enabled=$3,representative_resource_id=$4,updated_at=now() WHERE id=$1`, collectionID, revisionID, enabled, representativeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) CollectionReviewQueue(ctx context.Context) ([]Collection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT c.owner_id::text,c.id::text FROM resource_collection_revisions cr JOIN resource_collections c ON c.id=cr.collection_id WHERE cr.state='pending' ORDER BY c.id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pair struct{ owner, id string }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.owner, &p.id); err != nil {
			return nil, err
		}
		pairs = append(pairs, p)
	}
	items := make([]Collection, 0, len(pairs))
	for _, p := range pairs {
		item, err := s.Collection(ctx, p.owner, p.id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) PublicCollections(ctx context.Context, kind string) ([]PublicCollection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id::text,c.slug,cr.name,cr.summary,c.kind,u.username,u.bandbbs_user_id,u.avatar_url,COALESCE(c.representative_resource_id::text,''),
(SELECT count(*) FROM resources r WHERE r.collection_id=c.id AND r.moderation_state='visible' AND r.current_revision_id IS NOT NULL),
COALESCE((SELECT sum(v.coins) FROM resources r JOIN resource_coin_votes v ON v.resource_id=r.id AND v.invalidated_at IS NULL WHERE r.collection_id=c.id),0),c.updated_at
FROM resource_collections c JOIN resource_collection_revisions cr ON cr.id=c.current_revision_id JOIN users u ON u.id=c.owner_id
WHERE c.enabled AND ($1='' OR c.kind=$1) AND EXISTS(SELECT 1 FROM resources r WHERE r.collection_id=c.id AND r.moderation_state='visible' AND r.current_revision_id IS NOT NULL)
ORDER BY c.updated_at DESC`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PublicCollection
	for rows.Next() {
		var item PublicCollection
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Summary, &item.Kind, &item.Owner, &item.OwnerBandBBSUserID, &item.OwnerAvatarURL, &item.RepresentativeResourceID, &item.ResourceCount, &item.CoinCount, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if representative, err := s.PublicResource(ctx, item.RepresentativeResourceID); err == nil {
			copy := representative.PublicResource
			item.Representative = &copy
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) PublicCollection(ctx context.Context, collectionID string) (PublicCollection, error) {
	var item PublicCollection
	err := s.db.QueryRowContext(ctx, `SELECT c.id::text,c.slug,cr.name,cr.summary,c.kind,u.username,u.bandbbs_user_id,u.avatar_url,COALESCE(c.representative_resource_id::text,''),
(SELECT count(*) FROM resources r WHERE r.collection_id=c.id AND r.moderation_state='visible' AND r.current_revision_id IS NOT NULL),
COALESCE((SELECT sum(v.coins) FROM resources r JOIN resource_coin_votes v ON v.resource_id=r.id AND v.invalidated_at IS NULL WHERE r.collection_id=c.id),0),c.updated_at
FROM resource_collections c JOIN resource_collection_revisions cr ON cr.id=c.current_revision_id JOIN users u ON u.id=c.owner_id
WHERE c.id=$1 AND c.enabled AND EXISTS(SELECT 1 FROM resources r WHERE r.collection_id=c.id AND r.moderation_state='visible' AND r.current_revision_id IS NOT NULL)`, collectionID).
		Scan(&item.ID, &item.Slug, &item.Name, &item.Summary, &item.Kind, &item.Owner, &item.OwnerBandBBSUserID, &item.OwnerAvatarURL, &item.RepresentativeResourceID, &item.ResourceCount, &item.CoinCount, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicCollection{}, ErrNotFound
	}
	if err != nil {
		return PublicCollection{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM resources WHERE collection_id=$1 AND moderation_state='visible' AND current_revision_id IS NOT NULL ORDER BY collection_position,created_at`, collectionID)
	if err != nil {
		return PublicCollection{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var resourceID string
		if err := rows.Scan(&resourceID); err != nil {
			return PublicCollection{}, err
		}
		resource, err := s.PublicResource(ctx, resourceID)
		if err != nil {
			return PublicCollection{}, err
		}
		item.Resources = append(item.Resources, resource.PublicResource)
	}
	for i := range item.Resources {
		if item.Resources[i].ID == item.RepresentativeResourceID {
			copy := item.Resources[i]
			item.Representative = &copy
			break
		}
	}
	return item, rows.Err()
}
