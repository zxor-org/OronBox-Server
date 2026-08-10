package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AdminCollectionQuery struct {
	Search  string
	Owner   string
	Kind    string
	State   string
	Sort    string
	Page    int
	PerPage int
}

func (query AdminCollectionQuery) normalized() AdminCollectionQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.Owner = strings.TrimSpace(query.Owner)
	query.Kind = strings.ToLower(strings.TrimSpace(query.Kind))
	query.State = strings.ToLower(strings.TrimSpace(query.State))
	query.Sort = strings.ToLower(strings.TrimSpace(query.Sort))
	if query.Kind != "quickapp" && query.Kind != "watchface" {
		query.Kind = ""
	}
	switch query.State {
	case "pending", "approved", "rejected", "superseded":
	default:
		query.State = ""
	}
	switch query.Sort {
	case "updated_asc", "created_desc", "name", "owner", "members_desc":
	default:
		query.Sort = "updated_desc"
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = 25
	}
	if query.PerPage > 100 {
		query.PerPage = 100
	}
	return query
}

type AdminCollectionItem struct {
	ID                       string
	OwnerID                  string
	Owner                    string
	Slug                     string
	Platform                 string
	Kind                     string
	CurrentRevisionID        string
	CurrentRevisionNumber    int
	CurrentRevisionName      string
	LatestRevisionID         string
	LatestRevisionNumber     int
	LatestRevisionName       string
	LatestRevisionSummary    string
	LatestRevisionState      string
	RepresentativeResourceID string
	Enabled                  bool
	MemberCount              int
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type AdminCollectionPage struct {
	Items      []AdminCollectionItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminCollectionQuery
}

type AdminCollectionRevision struct {
	ID                       string
	CollectionID             string
	Number                   int
	Name                     string
	Summary                  string
	State                    string
	ReviewerID               string
	Reviewer                 string
	ReviewNote               string
	Enabled                  bool
	RepresentativeResourceID string
	CreatedBy                string
	CreatedVia               string
	BaseRevisionID           string
	Members                  []AdminCollectionMember
	CreatedAt                time.Time
	UpdatedAt                time.Time
}

type AdminCollectionMember struct {
	ID                  string
	OwnerID             string
	Owner               string
	Slug                string
	Kind                string
	Position            int
	ModerationState     string
	CurrentRevisionID   string
	CurrentRevisionNo   int
	CurrentRevisionName string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type AdminCollectionDetail struct {
	Collection AdminCollectionItem
	Revisions  []AdminCollectionRevision
	Members    []AdminCollectionMember
}

type AdminCollectionMetadataInput struct {
	Name                     string
	Summary                  string
	Enabled                  bool
	RepresentativeResourceID string
	ResourceIDs              []string
	CreatedBy                string
}

func (input AdminCollectionMetadataInput) normalized() (AdminCollectionMetadataInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Summary = strings.TrimSpace(input.Summary)
	input.RepresentativeResourceID = strings.TrimSpace(input.RepresentativeResourceID)
	input.CreatedBy = strings.TrimSpace(input.CreatedBy)
	if input.Name == "" || len(input.Name) > 120 || len(input.Summary) > 4000 {
		return input, fmt.Errorf("%w: collection metadata", ErrAdminResourceConflict)
	}
	seen := make(map[string]bool, len(input.ResourceIDs))
	for index := range input.ResourceIDs {
		input.ResourceIDs[index] = strings.TrimSpace(input.ResourceIDs[index])
		if _, err := uuid.Parse(input.ResourceIDs[index]); err != nil || seen[input.ResourceIDs[index]] {
			return input, fmt.Errorf("%w: collection members", ErrAdminResourceConflict)
		}
		seen[input.ResourceIDs[index]] = true
	}
	if input.RepresentativeResourceID == "" && len(input.ResourceIDs) > 0 {
		input.RepresentativeResourceID = input.ResourceIDs[0]
	}
	if input.RepresentativeResourceID != "" && !seen[input.RepresentativeResourceID] {
		return input, fmt.Errorf("%w: representative must be a member", ErrAdminResourceConflict)
	}
	return input, nil
}

const adminCollectionsFromSQL = `FROM resource_collections collection
JOIN users owner ON owner.id=collection.owner_id
LEFT JOIN resource_collection_revisions current_revision ON current_revision.id=collection.current_revision_id
LEFT JOIN LATERAL (
 SELECT revision.* FROM resource_collection_revisions revision
 WHERE revision.collection_id=collection.id
 ORDER BY revision.revision_no DESC LIMIT 1
) latest_revision ON TRUE`

func (s *Store) AdminCollections(ctx context.Context, raw AdminCollectionQuery) (AdminCollectionPage, error) {
	query := raw.normalized()
	filter := `WHERE ($1='' OR collection.id::text ILIKE '%'||$1||'%' OR collection.slug ILIKE '%'||$1||'%' OR COALESCE(latest_revision.name,'') ILIKE '%'||$1||'%' OR COALESCE(latest_revision.summary,'') ILIKE '%'||$1||'%')
 AND ($2='' OR owner.id::text=$2 OR owner.username ILIKE '%'||$2||'%')
 AND ($3='' OR collection.kind=$3)
 AND ($4='' OR latest_revision.state=$4)`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+adminCollectionsFromSQL+` `+filter,
		query.Search, query.Owner, query.Kind, query.State).Scan(&total); err != nil {
		return AdminCollectionPage{}, err
	}
	order := map[string]string{
		"updated_desc": "collection.updated_at DESC, collection.id DESC",
		"updated_asc":  "collection.updated_at ASC, collection.id ASC",
		"created_desc": "collection.created_at DESC, collection.id DESC",
		"name":         "lower(COALESCE(latest_revision.name,collection.slug)), collection.id",
		"owner":        "lower(owner.username), collection.updated_at DESC, collection.id",
		"members_desc": "(SELECT count(*) FROM resources member WHERE member.collection_id=collection.id) DESC, collection.updated_at DESC, collection.id",
	}[query.Sort]
	rows, err := s.db.QueryContext(ctx, `SELECT collection.id::text,collection.owner_id::text,owner.username,collection.slug,collection.platform,collection.kind,
 COALESCE(current_revision.id::text,''),COALESCE(current_revision.revision_no,0),COALESCE(current_revision.name,''),
 COALESCE(latest_revision.id::text,''),COALESCE(latest_revision.revision_no,0),COALESCE(latest_revision.name,''),COALESCE(latest_revision.summary,''),COALESCE(latest_revision.state,''),
 COALESCE(collection.representative_resource_id::text,''),collection.enabled,(SELECT count(*) FROM resources member WHERE member.collection_id=collection.id),
 collection.created_at,collection.updated_at
`+adminCollectionsFromSQL+` `+filter+` ORDER BY `+order+` LIMIT $5 OFFSET $6`,
		query.Search, query.Owner, query.Kind, query.State, query.PerPage, (query.Page-1)*query.PerPage)
	if err != nil {
		return AdminCollectionPage{}, err
	}
	defer rows.Close()
	page := AdminCollectionPage{Items: []AdminCollectionItem{}, Total: total, Page: query.Page, PerPage: query.PerPage, Query: query}
	for rows.Next() {
		var item AdminCollectionItem
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Owner, &item.Slug, &item.Platform, &item.Kind,
			&item.CurrentRevisionID, &item.CurrentRevisionNumber, &item.CurrentRevisionName,
			&item.LatestRevisionID, &item.LatestRevisionNumber, &item.LatestRevisionName, &item.LatestRevisionSummary, &item.LatestRevisionState,
			&item.RepresentativeResourceID, &item.Enabled, &item.MemberCount, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return AdminCollectionPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminCollectionPage{}, err
	}
	page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	return page, nil
}

func (s *Store) AdminCollection(ctx context.Context, id string) (AdminCollectionDetail, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminCollectionDetail{}, ErrAdminResourceNotFound
	}
	var item AdminCollectionItem
	err := s.db.QueryRowContext(ctx, `SELECT collection.id::text,collection.owner_id::text,owner.username,collection.slug,collection.platform,collection.kind,
 COALESCE(current_revision.id::text,''),COALESCE(current_revision.revision_no,0),COALESCE(current_revision.name,''),
 COALESCE(latest_revision.id::text,''),COALESCE(latest_revision.revision_no,0),COALESCE(latest_revision.name,''),COALESCE(latest_revision.summary,''),COALESCE(latest_revision.state,''),
 COALESCE(collection.representative_resource_id::text,''),collection.enabled,(SELECT count(*) FROM resources member WHERE member.collection_id=collection.id),
 collection.created_at,collection.updated_at
`+adminCollectionsFromSQL+` WHERE collection.id=$1`, id).Scan(
		&item.ID, &item.OwnerID, &item.Owner, &item.Slug, &item.Platform, &item.Kind,
		&item.CurrentRevisionID, &item.CurrentRevisionNumber, &item.CurrentRevisionName,
		&item.LatestRevisionID, &item.LatestRevisionNumber, &item.LatestRevisionName, &item.LatestRevisionSummary, &item.LatestRevisionState,
		&item.RepresentativeResourceID, &item.Enabled, &item.MemberCount, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminCollectionDetail{}, ErrAdminResourceNotFound
	}
	if err != nil {
		return AdminCollectionDetail{}, err
	}
	detail := AdminCollectionDetail{Collection: item, Revisions: []AdminCollectionRevision{}, Members: []AdminCollectionMember{}}
	revisions, err := s.db.QueryContext(ctx, `SELECT revision.id::text,revision.collection_id::text,revision.revision_no,revision.name,revision.summary,revision.state,
	COALESCE(revision.reviewer_id::text,''),COALESCE(reviewer.username,''),revision.review_note,revision.enabled,COALESCE(revision.representative_resource_id::text,''),COALESCE(revision.created_by::text,''),revision.created_via,COALESCE(revision.base_revision_id::text,''),revision.created_at,revision.updated_at
FROM resource_collection_revisions revision
LEFT JOIN users reviewer ON reviewer.id=revision.reviewer_id
WHERE revision.collection_id=$1 ORDER BY revision.revision_no DESC`, id)
	if err != nil {
		return AdminCollectionDetail{}, err
	}
	for revisions.Next() {
		var revision AdminCollectionRevision
		if err := revisions.Scan(&revision.ID, &revision.CollectionID, &revision.Number, &revision.Name, &revision.Summary, &revision.State,
			&revision.ReviewerID, &revision.Reviewer, &revision.ReviewNote, &revision.Enabled, &revision.RepresentativeResourceID, &revision.CreatedBy, &revision.CreatedVia, &revision.BaseRevisionID, &revision.CreatedAt, &revision.UpdatedAt); err != nil {
			revisions.Close()
			return AdminCollectionDetail{}, err
		}
		detail.Revisions = append(detail.Revisions, revision)
	}
	if err := revisions.Err(); err != nil {
		revisions.Close()
		return AdminCollectionDetail{}, err
	}
	if err := revisions.Close(); err != nil {
		return AdminCollectionDetail{}, err
	}
	for index := range detail.Revisions {
		snapshot, err := s.adminCollectionRevisionMembers(ctx, detail.Revisions[index].ID)
		if err != nil {
			return AdminCollectionDetail{}, err
		}
		detail.Revisions[index].Members = snapshot
	}
	members, err := s.db.QueryContext(ctx, `SELECT resource.id::text,resource.owner_id::text,owner.username,resource.slug,resource.kind,resource.collection_position,resource.moderation_state,
 COALESCE(current_revision.id::text,''),COALESCE(current_revision.revision_no,0),COALESCE(current_revision.name,''),resource.created_at,resource.updated_at
FROM resources resource
JOIN users owner ON owner.id=resource.owner_id
LEFT JOIN resource_revisions current_revision ON current_revision.id=resource.current_revision_id
WHERE resource.collection_id=$1 ORDER BY resource.collection_position,resource.created_at,resource.id`, id)
	if err != nil {
		return AdminCollectionDetail{}, err
	}
	defer members.Close()
	for members.Next() {
		var member AdminCollectionMember
		if err := members.Scan(&member.ID, &member.OwnerID, &member.Owner, &member.Slug, &member.Kind, &member.Position, &member.ModerationState,
			&member.CurrentRevisionID, &member.CurrentRevisionNo, &member.CurrentRevisionName, &member.CreatedAt, &member.UpdatedAt); err != nil {
			return AdminCollectionDetail{}, err
		}
		detail.Members = append(detail.Members, member)
	}
	if err := members.Err(); err != nil {
		return AdminCollectionDetail{}, err
	}
	return detail, nil
}

func (s *Store) adminCollectionRevisionMembers(ctx context.Context, revisionID string) ([]AdminCollectionMember, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(resource.id::text,''),COALESCE(resource.owner_id::text,''),COALESCE(owner.username,''),snapshot.resource_slug,COALESCE(resource.kind,''),snapshot.position,COALESCE(resource.moderation_state,'deleted'),COALESCE(current_revision.id::text,''),COALESCE(current_revision.revision_no,0),COALESCE(current_revision.name,snapshot.resource_name),COALESCE(resource.created_at,revision.created_at),COALESCE(resource.updated_at,revision.created_at) FROM resource_collection_revision_members snapshot JOIN resource_collection_revisions revision ON revision.id=snapshot.revision_id LEFT JOIN resources resource ON resource.id=snapshot.resource_id LEFT JOIN users owner ON owner.id=resource.owner_id LEFT JOIN resource_revisions current_revision ON current_revision.id=resource.current_revision_id WHERE snapshot.revision_id=$1 ORDER BY snapshot.position`, revisionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AdminCollectionMember{}
	for rows.Next() {
		var item AdminCollectionMember
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Owner, &item.Slug, &item.Kind, &item.Position, &item.ModerationState, &item.CurrentRevisionID, &item.CurrentRevisionNo, &item.CurrentRevisionName, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// AdminUpdateCollectionMetadata creates a complete immutable management
// revision. No live collection field or membership changes before approval.
func (s *Store) AdminUpdateCollectionMetadata(ctx context.Context, collectionID string, raw AdminCollectionMetadataInput) (AdminCollectionRevision, error) {
	if _, err := uuid.Parse(collectionID); err != nil {
		return AdminCollectionRevision{}, ErrAdminResourceNotFound
	}
	input, err := raw.normalized()
	if err != nil {
		return AdminCollectionRevision{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AdminCollectionRevision{}, err
	}
	defer tx.Rollback()
	var lockedID, ownerID, kind, baseRevisionID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text,owner_id::text,kind,COALESCE(current_revision_id::text,'') FROM resource_collections WHERE id=$1 FOR UPDATE`, collectionID).Scan(&lockedID, &ownerID, &kind, &baseRevisionID); errors.Is(err, sql.ErrNoRows) {
		return AdminCollectionRevision{}, ErrAdminResourceNotFound
	} else if err != nil {
		return AdminCollectionRevision{}, err
	}
	var next int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM resource_collection_revisions WHERE collection_id=$1`, collectionID).Scan(&next); err != nil {
		return AdminCollectionRevision{}, err
	}
	for _, resourceID := range input.ResourceIDs {
		var valid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resources WHERE id=$1 AND owner_id=$2 AND kind=$3 AND (collection_id IS NULL OR collection_id=$4))`, resourceID, ownerID, kind, collectionID).Scan(&valid); err != nil {
			return AdminCollectionRevision{}, err
		}
		if !valid {
			return AdminCollectionRevision{}, fmt.Errorf("%w: incompatible collection member %s", ErrAdminResourceConflict, resourceID)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_collection_revisions SET state='superseded',updated_at=now() WHERE collection_id=$1 AND state='pending'`, collectionID); err != nil {
		return AdminCollectionRevision{}, err
	}
	revision := AdminCollectionRevision{ID: uuid.NewString(), CollectionID: collectionID, Number: next, Name: input.Name, Summary: input.Summary, State: "pending", Enabled: input.Enabled, RepresentativeResourceID: input.RepresentativeResourceID, CreatedVia: "admin", BaseRevisionID: baseRevisionID}
	if err := tx.QueryRowContext(ctx, `INSERT INTO resource_collection_revisions(id,collection_id,revision_no,name,summary,state,enabled,representative_resource_id,created_by,created_via,base_revision_id)
VALUES($1,$2,$3,$4,$5,'pending',$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,'admin',NULLIF($9,'')::uuid)
RETURNING created_at,updated_at`, revision.ID, collectionID, revision.Number, revision.Name, revision.Summary, revision.Enabled, revision.RepresentativeResourceID, input.CreatedBy, revision.BaseRevisionID).Scan(&revision.CreatedAt, &revision.UpdatedAt); err != nil {
		return AdminCollectionRevision{}, err
	}
	for position, resourceID := range input.ResourceIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_collection_revision_members(id,revision_id,resource_id,resource_slug,resource_name,position) SELECT $1,$2,resource.id,resource.slug,COALESCE(current.name,resource.draft_name),$4 FROM resources resource LEFT JOIN resource_revisions current ON current.id=resource.current_revision_id WHERE resource.id=$3`, uuid.NewString(), revision.ID, resourceID, position); err != nil {
			return AdminCollectionRevision{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_collections SET updated_at=now() WHERE id=$1`, collectionID); err != nil {
		return AdminCollectionRevision{}, err
	}
	if err := tx.Commit(); err != nil {
		return AdminCollectionRevision{}, err
	}
	return revision, nil
}
