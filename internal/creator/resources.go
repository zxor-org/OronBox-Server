package creator

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
)

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
	var drafts int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM resources r WHERE r.owner_id=$1 AND NOT EXISTS(SELECT 1 FROM resource_revisions rr WHERE rr.resource_id=r.id AND rr.state<>'draft')`, ownerID).Scan(&drafts); err != nil {
		return Workspace{}, err
	}
	if drafts >= maxDraftResources {
		return Workspace{}, fmt.Errorf("%w: draft resource limit of %d reached", ErrConflict, maxDraftResources)
	}
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
	rows, err := s.db.QueryContext(ctx, `SELECT id::text FROM resources WHERE owner_id=$1 AND moderation_state<>'deleted' ORDER BY updated_at DESC`, ownerID)
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
	err := s.db.QueryRowContext(ctx, `SELECT id::text,owner_id::text,slug,draft_name,kind,curation_grade,COALESCE(collection_id::text,''),collection_position,moderation_state,COALESCE(moderation_by,''),moderation_reason,moderation_at,download_count,COALESCE(current_revision_id::text,''),created_at,updated_at,edit_version FROM resources WHERE id=$1 AND owner_id=$2`, resourceID, ownerID).
		Scan(&result.Resource.ID, &result.Resource.OwnerID, &result.Resource.Slug, &result.Resource.DraftName, &result.Resource.Kind, &result.Resource.CurationGrade, &result.Resource.CollectionID, &result.Resource.CollectionPosition, &result.Resource.ModerationState, &result.Resource.ModerationBy, &result.Resource.ModerationReason, &result.Resource.ModerationAt, &result.Resource.DownloadCount, &result.Resource.CurrentRevisionID, &result.Resource.CreatedAt, &result.Resource.UpdatedAt, &result.Resource.EditVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,resource_id::text,revision_no,name,summary,description,description_format,paid_type,purchase_link,purchase_price,CASE WHEN COALESCE(purchase_link,'') <> '' THEN 'CNY' ELSE '' END,state,publication_plan,created_at,binding_claims FROM resource_revisions WHERE resource_id=$1 ORDER BY revision_no DESC`, resourceID)
	if err != nil {
		return Workspace{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var revision Revision
		var plan, claims []byte
		var purchasePrice sql.NullFloat64
		if err := rows.Scan(&revision.ID, &revision.ResourceID, &revision.Number, &revision.Name, &revision.Summary, &revision.Description, &revision.DescriptionFormat, &revision.PaidType, &revision.PurchaseLink, &purchasePrice, &revision.PurchaseCurrency, &revision.State, &plan, &revision.CreatedAt, &claims); err != nil {
			return Workspace{}, err
		}
		if purchasePrice.Valid {
			value := purchasePrice.Float64
			revision.PurchasePrice = &value
		}
		revision.BindingClaims = []ImportedBinding{}
		if err := json.Unmarshal(claims, &revision.BindingClaims); err != nil {
			return Workspace{}, err
		}
		revision.PublicationPlan = []PublicationRequest{}
		_ = json.Unmarshal(plan, &revision.PublicationPlan)
		attributeRows, err := s.db.QueryContext(ctx, `SELECT attribute FROM resource_revision_attributes WHERE revision_id=$1 ORDER BY attribute`, revision.ID)
		if err != nil {
			return Workspace{}, err
		}
		for attributeRows.Next() {
			var attribute string
			if err := attributeRows.Scan(&attribute); err != nil {
				attributeRows.Close()
				return Workspace{}, err
			}
			revision.Attributes = append(revision.Attributes, attribute)
		}
		attributeRows.Close()
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
		result.Links, err = s.revisionLinks(ctx, latest.ID)
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
	bindingRows, err := s.db.QueryContext(ctx, `SELECT provider,external_id,external_url,meta FROM external_bindings WHERE resource_id=$1 ORDER BY provider`, resourceID)
	if err != nil {
		return Workspace{}, err
	}
	defer bindingRows.Close()
	for bindingRows.Next() {
		var binding ExternalBinding
		var meta []byte
		if err := bindingRows.Scan(&binding.Provider, &binding.ExternalID, &binding.ExternalURL, &meta); err != nil {
			return Workspace{}, err
		}
		_ = json.Unmarshal(meta, &binding.Meta)
		result.Bindings = append(result.Bindings, binding)
	}
	if err := bindingRows.Err(); err != nil {
		return Workspace{}, err
	}
	var active ActiveAstroBoxSubmission
	var number string
	err = s.db.QueryRowContext(ctx, `SELECT c.id::text,rr.id::text,COALESCE(p.status_detail->>'pull_request_number',''),COALESCE(p.external_url,''),COALESCE(p.status_detail->>'review_state','waiting_review') FROM publications p JOIN resource_revisions rr ON rr.id=p.revision_id JOIN review_cases c ON c.revision_id=rr.id WHERE rr.resource_id=$1 AND p.target='astrobox' AND p.state='reviewing' ORDER BY rr.revision_no DESC LIMIT 1`, resourceID).Scan(&active.ReviewID, &active.RevisionID, &number, &active.ExternalURL, &active.ReviewState)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, err
	}
	if err == nil {
		active.PullRequestNumber, _ = strconv.Atoi(number)
		if active.PullRequestNumber > 0 {
			result.ActiveAstroBoxSubmission = &active
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
	if moderationState == "deleted" {
		return Workspace{}, fmt.Errorf("%w: deleted resources cannot be modified", ErrConflict)
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

type DeleteResult struct {
	OperationID       string `json:"operation_id"`
	State             string `json:"state"`
	BandBBSDeleted    int    `json:"bandbbs_deleted"`
	AstroBoxRemovalPR string `json:"astrobox_removal_pr"`
}

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
	if json.Unmarshal([]byte(bound), &mapped) != nil {
		return nil
	}
	for _, id := range mapped {
		add(id)
	}
	return ids
}
