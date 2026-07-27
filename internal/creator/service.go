package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"github.com/zxor-org/OronBox-Server/internal/observability"
)

var (
	ErrNotFound = errors.New("creator resource not found")
	ErrConflict = errors.New("creator resource changed")
	ErrInvalid  = errors.New("invalid creator resource")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

type Limits struct {
	UploadMaxBytes  int64
	PreviewMaxBytes int64
	PreviewMaxCount int
}

type Service struct {
	db     *sql.DB
	blobs  blob.Store
	limits Limits
	now    func() time.Time
	// BandBBSDelete synchronously deletes the given BandBBS resource ids on
	// behalf of ownerID. Delete aborts when it reports an error.
	BandBBSDelete func(ctx context.Context, ownerID string, resourceIDs []string) error
}

func New(db *sql.DB, blobs blob.Store, limits Limits) *Service {
	return &Service{db: db, blobs: blobs, limits: limits, now: func() time.Time { return time.Now().UTC() }}
}

// log keeps request-scoped fields (request_id, path) but files the event
// under the creator component.
func log(ctx context.Context) *slog.Logger {
	return observability.From(ctx).With("component", "creator")
}

func (s *Service) Create(ctx context.Context, ownerID, slug, name string, kind ResourceKind) (Workspace, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	name = strings.TrimSpace(name)
	if !slugPattern.MatchString(slug) || !kind.Valid() || name == "" || len([]rune(name)) > 120 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return Workspace{}, fmt.Errorf("%w: slug, name, or kind", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()
	resourceID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,draft_name,kind) VALUES($1,$2,$3,$4,$5)`, resourceID, ownerID, slug, name, kind); err != nil {
		return Workspace{}, err
	}
	if err = event(ctx, tx, resourceID, ownerID, "resource.created", map[string]any{"kind": kind}); err != nil {
		return Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workspace{}, err
	}
	log(ctx).Info("resource created", "resource_id", resourceID, "owner_id", ownerID, "kind", kind, "slug", slug)
	return s.Workspace(ctx, ownerID, resourceID)
}

func (s *Service) List(ctx context.Context, ownerID string) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM resources WHERE owner_id=$1 ORDER BY updated_at DESC`, ownerID)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := make([]Workspace, 0, len(ids))
	for _, id := range ids {
		workspace, err := s.Workspace(ctx, ownerID, id)
		if err != nil {
			return nil, err
		}
		result = append(result, workspace)
	}
	return result, nil
}

// Workspace returns the resource with the assets of its latest revision,
// which is the editing baseline for the next publish.
func (s *Service) Workspace(ctx context.Context, ownerID, resourceID string) (Workspace, error) {
	var result Workspace
	err := s.db.QueryRowContext(ctx, `SELECT id::text,owner_id::text,slug,draft_name,kind,moderation_state,COALESCE(moderation_by,''),moderation_reason,moderation_at,download_count,COALESCE(current_revision_id::text,''),created_at,updated_at FROM resources WHERE id=$1 AND owner_id=$2`, resourceID, ownerID).
		Scan(&result.Resource.ID, &result.Resource.OwnerID, &result.Resource.Slug, &result.Resource.DraftName, &result.Resource.Kind, &result.Resource.ModerationState, &result.Resource.ModerationBy, &result.Resource.ModerationReason, &result.Resource.ModerationAt, &result.Resource.DownloadCount, &result.Resource.CurrentRevisionID, &result.Resource.CreatedAt, &result.Resource.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,resource_id::text,revision_no,name,summary,state,created_at FROM resource_revisions WHERE resource_id=$1 ORDER BY revision_no DESC`, resourceID)
	if err != nil {
		return Workspace{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var revision Revision
		if err := rows.Scan(&revision.ID, &revision.ResourceID, &revision.Number, &revision.Name, &revision.Summary, &revision.State, &revision.CreatedAt); err != nil {
			return Workspace{}, err
		}
		result.Revisions = append(result.Revisions, revision)
		if revision.ID == result.Resource.CurrentRevisionID {
			copy := revision
			result.CurrentRevision = &copy
		}
	}
	if err := rows.Err(); err != nil {
		return Workspace{}, err
	}
	if len(result.Revisions) > 0 {
		latest := result.Revisions[0]
		result.Artifacts, err = s.revisionArtifacts(ctx, latest.ID, false)
		if err != nil {
			return Workspace{}, err
		}
		result.Media, err = s.revisionMedia(ctx, latest.ID)
		if err != nil {
			return Workspace{}, err
		}
		var review ReviewCase
		err = s.db.QueryRowContext(ctx, `SELECT id::text,revision_id::text,state,note,created_at,updated_at FROM review_cases WHERE revision_id=$1`, latest.ID).
			Scan(&review.ID, &review.RevisionID, &review.State, &review.Note, &review.CreatedAt, &review.UpdatedAt)
		if err == nil {
			result.Review = &review
		} else if !errors.Is(err, sql.ErrNoRows) {
			return Workspace{}, err
		}
		result.Publications, err = s.publications(ctx, latest.ID)
		if err != nil {
			return Workspace{}, err
		}
	}
	return result, nil
}

// SetModeration applies the creator-side moderation transitions: "takedown"
// hides the resource from the public catalog (visible -> suspended by owner)
// and "restore" brings an owner-suspended resource back. Takedown cancels the
// resource's pending/running publications, the same cascading effect as the
// admin suspend action. Frozen and admin-suspended resources are out of the
// creator's reach.
func (s *Service) SetModeration(ctx context.Context, ownerID, resourceID, action string) (Workspace, error) {
	if action != "takedown" && action != "restore" {
		return Workspace{}, fmt.Errorf("%w: unknown moderation action %q", ErrInvalid, action)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()
	var moderationState, moderationBy string
	err = tx.QueryRowContext(ctx, `SELECT moderation_state,COALESCE(moderation_by,'') FROM resources WHERE id=$1 AND owner_id=$2 FOR UPDATE`, resourceID, ownerID).Scan(&moderationState, &moderationBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	eventType := ""
	switch action {
	case "takedown":
		if moderationState == "frozen" {
			return Workspace{}, fmt.Errorf("%w: resource is frozen by an administrator", ErrConflict)
		}
		if moderationState == "suspended" && moderationBy != "owner" {
			return Workspace{}, fmt.Errorf("%w: resource is suspended by an administrator", ErrConflict)
		}
		eventType = "resource.suspended"
	case "restore":
		if moderationState != "suspended" || moderationBy != "owner" {
			return Workspace{}, fmt.Errorf("%w: only an owner-suspended resource can be restored by its creator", ErrConflict)
		}
		eventType = "resource.restored"
	}
	if action == "takedown" && moderationState != "suspended" {
		if _, err = tx.ExecContext(ctx, `UPDATE publications publication SET state='cancelled',error_message='cancelled by resource moderation',updated_at=now() FROM resource_revisions revision WHERE publication.revision_id=revision.id AND revision.resource_id=$1 AND publication.state IN ('pending','running')`, resourceID); err != nil {
			return Workspace{}, err
		}
	}
	if action == "takedown" {
		_, err = tx.ExecContext(ctx, `UPDATE resources SET moderation_state='suspended',moderation_by='owner',moderation_reason='',moderation_at=now(),updated_at=now() WHERE id=$1`, resourceID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE resources SET moderation_state='visible',moderation_by=NULL,moderation_reason='',moderation_at=NULL,updated_at=now() WHERE id=$1`, resourceID)
	}
	if err != nil {
		return Workspace{}, err
	}
	if err = event(ctx, tx, resourceID, ownerID, eventType, map[string]any{"actor": "owner", "action": action, "previous_moderation": moderationState, "previous_moderation_by": moderationBy}); err != nil {
		return Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workspace{}, err
	}
	log(ctx).Info("resource moderation changed", "resource_id", resourceID, "owner_id", ownerID, "action", action, "previous_moderation", moderationState)
	return s.Workspace(ctx, ownerID, resourceID)
}

func (s *Service) Delete(ctx context.Context, ownerID, resourceID string) error {
	var owned bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resources WHERE id=$1 AND owner_id=$2)`, resourceID, ownerID).Scan(&owned); err != nil {
		return err
	}
	if !owned {
		return ErrNotFound
	}
	var bound string
	err := s.db.QueryRowContext(ctx, `SELECT external_id FROM external_bindings WHERE resource_id=$1 AND provider='bandbbs'`, resourceID).Scan(&bound)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	ids := bandBBSResourceIDs(bound)
	if len(ids) > 0 {
		if s.BandBBSDelete == nil {
			return fmt.Errorf("BandBBS deletion is not configured")
		}
		if err := s.BandBBSDelete(ctx, ownerID, ids); err != nil {
			return err
		}
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM resources WHERE id=$1 AND owner_id=$2`, resourceID, ownerID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrConflict
	}
	log(ctx).Info("resource deleted", "resource_id", resourceID, "owner_id", ownerID, "bandbbs_resources", len(ids))
	return nil
}

// bandBBSResourceIDs reads the bound BandBBS resource ids. Current rows store
// a JSON object mapping category id to resource id; legacy rows stored a
// single bare resource id.
func bandBBSResourceIDs(bound string) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(value string) {
		if value != "" && !seen[value] {
			seen[value] = true
			ids = append(ids, value)
		}
	}
	mapped := map[string]string{}
	if json.Unmarshal([]byte(bound), &mapped) == nil {
		for _, id := range mapped {
			add(id)
		}
		return ids
	}
	add(bound)
	return ids
}

func (s *Service) Review(ctx context.Context, revisionID, reviewerID string, approve bool, note string, items []string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, revisionState := ReviewRejected, RevisionRejected
	if approve {
		state, revisionState = ReviewApproved, RevisionApproved
	}
	itemsJSON, _ := json.Marshal(items)
	result, err := tx.ExecContext(ctx, `UPDATE review_cases SET state=$2,reviewer_id=NULLIF($3,'')::uuid,note=$4,items=$5,updated_at=now() WHERE revision_id=$1 AND state='pending'`, revisionID, state, reviewerID, note, itemsJSON)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return ErrConflict
	}
	var resourceID string
	var revisionNo int
	if err = tx.QueryRowContext(ctx, `UPDATE resource_revisions SET state=$2 WHERE id=$1 RETURNING resource_id::text,revision_no`, revisionID, revisionState).Scan(&resourceID, &revisionNo); err != nil {
		return err
	}
	var restoredByReview bool
	if approve {
		if _, err = tx.ExecContext(ctx, `
UPDATE resource_revisions previous
SET state='superseded'
FROM resources resource,resource_revisions approved
WHERE approved.id=$1
  AND resource.id=approved.resource_id
  AND previous.id=resource.current_revision_id
  AND previous.id<>approved.id
  AND previous.state='approved'`, revisionID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE resources r SET current_revision_id=$1,updated_at=now() FROM resource_revisions rr WHERE rr.id=$1 AND r.id=rr.resource_id`, revisionID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE publications SET state=CASE WHEN target='oronbox' THEN 'published' ELSE 'pending' END,updated_at=now() WHERE revision_id=$1`, revisionID); err != nil {
			return err
		}
		// An approved revision makes the resource sellable again: lift a
		// suspension (owner takedown or admin action) so it returns to the
		// public catalog. Frozen resources stay locked until an admin acts.
		restored, err := tx.ExecContext(ctx, `UPDATE resources SET moderation_state='visible',moderation_by=NULL,moderation_reason='',moderation_at=NULL,updated_at=now() WHERE id=$1 AND moderation_state='suspended'`, resourceID)
		if err != nil {
			return err
		}
		if count, _ := restored.RowsAffected(); count == 1 {
			restoredByReview = true
			if err = event(ctx, tx, resourceID, reviewerID, "resource.restored", map[string]any{"trigger": "review_approved", "revision_id": revisionID, "revision_no": revisionNo, "previous_moderation": "suspended"}); err != nil {
				return err
			}
		}
	} else if _, err = tx.ExecContext(ctx, `UPDATE publications SET state='cancelled',updated_at=now() WHERE revision_id=$1`, revisionID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	event := "revision rejected"
	if approve {
		event = "revision approved"
	}
	log(ctx).Info(event, "resource_id", resourceID, "revision_id", revisionID, "revision_no", revisionNo, "reviewer_id", reviewerID, "has_note", strings.TrimSpace(note) != "")
	if restoredByReview {
		log(ctx).Info("resource restored by review approval", "resource_id", resourceID, "revision_id", revisionID, "reviewer_id", reviewerID)
	}
	return nil
}

func (s *Service) ReviewQueue(ctx context.Context) ([]Workspace, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT r.owner_id::text,r.id::text FROM review_cases c JOIN resource_revisions rr ON rr.id=c.revision_id JOIN resources r ON r.id=rr.resource_id WHERE c.state='pending' ORDER BY r.id::text`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pair struct{ owner, resource string }
	var pairs []pair
	for rows.Next() {
		var item pair
		if err := rows.Scan(&item.owner, &item.resource); err != nil {
			return nil, err
		}
		pairs = append(pairs, item)
	}
	result := make([]Workspace, 0, len(pairs))
	for _, item := range pairs {
		workspace, err := s.Workspace(ctx, item.owner, item.resource)
		if err != nil {
			return nil, err
		}
		result = append(result, workspace)
	}
	return result, rows.Err()
}

func (s *Service) Devices(ctx context.Context) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,codename,display_name,platform,astrobox_id,vendor FROM devices WHERE platform='vela_os' AND codename NOT IN ('m66','n69') ORDER BY vendor,display_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Device
	for rows.Next() {
		var device Device
		if err := rows.Scan(&device.ID, &device.Codename, &device.Name, &device.Platform, &device.AstroBoxID, &device.Vendor); err != nil {
			return nil, err
		}
		result = append(result, device)
	}
	return result, rows.Err()
}

// Pick the highest package version exposed by the revision.
const highestVersionSQL = `COALESCE((SELECT package_version FROM revision_artifacts WHERE revision_id=rr.id AND package_version<>'' ORDER BY length(package_version) DESC,package_version DESC LIMIT 1),'')`

func (s *Service) PublicResources(ctx context.Context, query PublicQuery) ([]PublicResource, int, error) {
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 50
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	order := "r.updated_at DESC"
	if query.Sort == "name" {
		order = "rr.name ASC"
	} else if query.Sort == "random" {
		order = "random()"
	}
	filter := `r.moderation_state='visible' AND r.current_revision_id IS NOT NULL AND ($1='' OR rr.name ILIKE '%'||$1||'%' OR rr.summary ILIKE '%'||$1||'%') AND ($2='' OR r.kind=$2) AND (cardinality($3::text[])=0 OR EXISTS(SELECT 1 FROM revision_artifacts a JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE a.revision_id=rr.id AND d.codename=ANY($3::text[])))`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM resources r JOIN resource_revisions rr ON rr.id=r.current_revision_id WHERE `+filter, query.Search, query.Kind, query.Devices).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id::text,r.slug,rr.name,rr.summary,u.username,u.bandbbs_user_id,u.avatar_url,r.kind,
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='preview' ORDER BY position LIMIT 1),''),
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='icon' ORDER BY position LIMIT 1),''),
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='cover' ORDER BY position LIMIT 1),''),
`+highestVersionSQL+`,
COALESCE((SELECT jsonb_agg(DISTINCT d.codename) FROM revision_artifacts a JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE a.revision_id=rr.id),'[]'),r.download_count,r.updated_at
FROM resources r JOIN resource_revisions rr ON rr.id=r.current_revision_id JOIN users u ON u.id=r.owner_id WHERE `+filter+` ORDER BY `+order+` LIMIT $4 OFFSET $5`, query.Search, query.Kind, query.Devices, query.Limit, query.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]PublicResource, 0)
	for rows.Next() {
		var item PublicResource
		var devices []byte
		if err := rows.Scan(&item.ID, &item.Slug, &item.Name, &item.Summary, &item.Owner, &item.OwnerBandBBSUserID, &item.OwnerAvatarURL, &item.Kind, &item.PreviewSHA256, &item.IconSHA256, &item.CoverSHA256, &item.Version, &devices, &item.DownloadCount, &item.UpdatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(devices, &item.Devices)
		result = append(result, item)
	}
	return result, total, rows.Err()
}

func (s *Service) PublicResource(ctx context.Context, resourceID string) (PublicResourceDetail, error) {
	var summary PublicResource
	var devices []byte
	var revisionID string
	err := s.db.QueryRowContext(ctx, `SELECT r.id::text,r.slug,rr.name,rr.summary,u.username,u.bandbbs_user_id,u.avatar_url,r.kind,
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='preview' ORDER BY position LIMIT 1),''),
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='icon' ORDER BY position LIMIT 1),''),
COALESCE((SELECT blob_sha256 FROM revision_media WHERE revision_id=rr.id AND role='cover' ORDER BY position LIMIT 1),''),
`+highestVersionSQL+`,
COALESCE((SELECT jsonb_agg(DISTINCT d.codename) FROM revision_artifacts a JOIN revision_artifact_devices b ON b.artifact_id=a.id JOIN devices d ON d.id=b.device_id WHERE a.revision_id=rr.id),'[]'),r.download_count,r.updated_at,rr.id::text
FROM resources r JOIN resource_revisions rr ON rr.id=r.current_revision_id JOIN users u ON u.id=r.owner_id WHERE r.id=$1 AND r.moderation_state='visible'`, resourceID).
		Scan(&summary.ID, &summary.Slug, &summary.Name, &summary.Summary, &summary.Owner, &summary.OwnerBandBBSUserID, &summary.OwnerAvatarURL, &summary.Kind, &summary.PreviewSHA256, &summary.IconSHA256, &summary.CoverSHA256, &summary.Version, &devices, &summary.DownloadCount, &summary.UpdatedAt, &revisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return PublicResourceDetail{}, ErrNotFound
	}
	if err != nil {
		return PublicResourceDetail{}, err
	}
	_ = json.Unmarshal(devices, &summary.Devices)
	media, err := s.revisionMedia(ctx, revisionID)
	if err != nil {
		return PublicResourceDetail{}, err
	}
	artifacts, err := s.revisionArtifacts(ctx, revisionID, true)
	if err != nil {
		return PublicResourceDetail{}, err
	}
	return PublicResourceDetail{PublicResource: summary, Media: media, Artifacts: artifacts}, nil
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
