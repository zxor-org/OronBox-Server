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
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary) VALUES($1,$2,1,$3,$4)`, revisionID, collectionID, name, summary); err != nil {
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
	var lockedID string
	if err = tx.QueryRowContext(ctx, `SELECT id::text FROM resource_collections WHERE id=$1 AND owner_id=$2 FOR UPDATE`, collectionID, ownerID).Scan(&lockedID); errors.Is(err, sql.ErrNoRows) {
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary) VALUES($1,$2,$3,$4,$5)`, uuid.NewString(), collectionID, next, name, summary); err != nil {
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
	err := scanner.Scan(&item.ID, &item.OwnerID, &item.Slug, &item.Platform, &item.Kind, &currentID, &representativeID, &item.ResourceCount, &item.TotalCoins, &item.CreatedAt, &item.UpdatedAt)
	if currentID.Valid {
		item.CurrentRevisionID = currentID.String
	}
	if representativeID.Valid {
		item.RepresentativeResourceID = representativeID.String
	}
	return item, err
}

const collectionSelect = `SELECT c.id::text,c.owner_id::text,c.slug,c.platform,c.kind,c.current_revision_id::text,c.representative_resource_id::text,
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
	err := s.db.QueryRowContext(ctx, `SELECT id::text,collection_id::text,revision_no,name,summary,state,review_note,created_at,updated_at FROM resource_collection_revisions WHERE id=$1`, revisionID).
		Scan(&item.ID, &item.CollectionID, &item.Number, &item.Name, &item.Summary, &item.State, &item.ReviewNote, &item.CreatedAt, &item.UpdatedAt)
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
	var kind string
	if err = tx.QueryRowContext(ctx, `SELECT kind FROM resource_collections WHERE id=$1 AND owner_id=$2 FOR UPDATE`, collectionID, ownerID).Scan(&kind); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	seen := make(map[string]bool, len(resourceIDs))
	if len(resourceIDs) > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE resource_collections previous SET representative_resource_id=(
SELECT remaining.id FROM resources remaining WHERE remaining.collection_id=previous.id AND NOT(remaining.id=ANY($2::uuid[])) ORDER BY remaining.collection_position,remaining.created_at LIMIT 1
),updated_at=now() WHERE previous.owner_id=$3 AND previous.id<>$1 AND previous.representative_resource_id=ANY($2::uuid[])`, collectionID, resourceIDs, ownerID); err != nil {
			return err
		}
	}
	for position, resourceID := range resourceIDs {
		if seen[resourceID] {
			return fmt.Errorf("%w: duplicate collection resource", ErrInvalid)
		}
		seen[resourceID] = true
		result, err := tx.ExecContext(ctx, `UPDATE resources SET collection_id=$1,collection_position=$2,updated_at=now() WHERE id=$3 AND owner_id=$4 AND kind=$5`, collectionID, position, resourceID, ownerID, kind)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count != 1 {
			return fmt.Errorf("%w: collection resource", ErrInvalid)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resources SET collection_id=NULL,collection_position=0,updated_at=now() WHERE collection_id=$1 AND NOT(id=ANY($2::uuid[]))`, collectionID, resourceIDs); err != nil {
		return err
	}
	if representativeID == "" && len(resourceIDs) > 0 {
		representativeID = resourceIDs[0]
	}
	if representativeID != "" && !seen[representativeID] {
		return fmt.Errorf("%w: representative must belong to collection", ErrInvalid)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resource_collections SET representative_resource_id=NULLIF($2,'')::uuid,updated_at=now() WHERE id=$1`, collectionID, representativeID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) DeleteCollection(ctx context.Context, ownerID, collectionID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM resource_collections WHERE id=$1 AND owner_id=$2`, collectionID, ownerID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrNotFound
	}
	return nil
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
		if _, err = tx.ExecContext(ctx, `UPDATE resource_collections SET current_revision_id=$2,updated_at=now() WHERE id=$1`, collectionID, revisionID); err != nil {
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
WHERE ($1='' OR c.kind=$1) AND EXISTS(SELECT 1 FROM resources r WHERE r.collection_id=c.id AND r.moderation_state='visible' AND r.current_revision_id IS NOT NULL)
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
WHERE c.id=$1 AND EXISTS(SELECT 1 FROM resources r WHERE r.collection_id=c.id AND r.moderation_state='visible' AND r.current_revision_id IS NOT NULL)`, collectionID).
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
