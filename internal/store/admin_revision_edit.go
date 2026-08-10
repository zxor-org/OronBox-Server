package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

type AdminRevisionDraftInput struct {
	DraftRevisionID string
	BaseRevisionID  string
	Name            string
	Summary         string
	PaidType        string
	Attributes      []string
	Links           []AdminLink
	PublicationPlan json.RawMessage
}

func (input AdminRevisionDraftInput) normalized() (AdminRevisionDraftInput, error) {
	input.DraftRevisionID = strings.TrimSpace(input.DraftRevisionID)
	input.BaseRevisionID = strings.TrimSpace(input.BaseRevisionID)
	input.Name = strings.TrimSpace(input.Name)
	input.Summary = strings.TrimSpace(input.Summary)
	input.PaidType = strings.TrimSpace(input.PaidType)
	if input.PaidType == "" {
		input.PaidType = "free"
	}
	if input.Name == "" || len(input.Name) > 120 || len(input.Summary) > 4000 {
		return input, fmt.Errorf("%w: revision metadata", ErrAdminResourceConflict)
	}
	if input.PaidType != "free" && input.PaidType != "paid" && input.PaidType != "force_paid" {
		return input, fmt.Errorf("%w: paid type", ErrAdminResourceConflict)
	}
	seen := make(map[string]bool, len(input.Attributes))
	attributes := make([]string, 0, len(input.Attributes))
	for _, attribute := range input.Attributes {
		attribute = strings.TrimSpace(attribute)
		if attribute == "" || seen[attribute] {
			continue
		}
		seen[attribute] = true
		attributes = append(attributes, attribute)
	}
	if len(attributes) > 32 {
		return input, fmt.Errorf("%w: resource attributes", ErrAdminResourceConflict)
	}
	input.Attributes = attributes
	if len(input.Links) > 16 {
		return input, fmt.Errorf("%w: resource links", ErrAdminResourceConflict)
	}
	for index := range input.Links {
		link := &input.Links[index]
		link.Position = index
		link.Title = strings.TrimSpace(link.Title)
		link.URL = strings.TrimSpace(link.URL)
		parsed, err := url.ParseRequestURI(link.URL)
		if err != nil || link.Title == "" || len(link.Title) > 80 || len(link.URL) > 2048 || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return input, fmt.Errorf("%w: resource links", ErrAdminResourceConflict)
		}
	}
	if len(input.PublicationPlan) == 0 {
		input.PublicationPlan = json.RawMessage("[]")
	}
	var plan []struct {
		Target string         `json:"target"`
		Config map[string]any `json:"config"`
	}
	if json.Unmarshal(input.PublicationPlan, &plan) != nil {
		return input, fmt.Errorf("%w: publication plan", ErrAdminResourceConflict)
	}
	targets := map[string]bool{}
	for _, publication := range plan {
		if targets[publication.Target] || (publication.Target != "oronbox" && publication.Target != "bandbbs" && publication.Target != "astrobox") {
			return input, fmt.Errorf("%w: publication plan", ErrAdminResourceConflict)
		}
		targets[publication.Target] = true
	}
	return input, nil
}

// AdminSaveRevisionDraft creates or updates an admin-authored draft. Creating
// a draft clones the selected revision's immutable assets and device bindings.
func (s *Store) AdminSaveRevisionDraft(ctx context.Context, resourceID string, raw AdminRevisionDraftInput, actor AdminSession) (string, error) {
	if _, err := uuid.Parse(resourceID); err != nil {
		return "", ErrAdminResourceNotFound
	}
	input, err := raw.normalized()
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT true FROM resources WHERE id=$1 FOR UPDATE`, resourceID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return "", ErrAdminResourceNotFound
	} else if err != nil {
		return "", err
	}
	if len(input.Attributes) > 0 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM resource_attributes WHERE id=ANY($1)`, input.Attributes).Scan(&count); err != nil {
			return "", err
		}
		if count != len(input.Attributes) {
			return "", fmt.Errorf("%w: unknown resource attribute", ErrAdminResourceConflict)
		}
	}

	revisionID := input.DraftRevisionID
	if revisionID != "" {
		if _, err := uuid.Parse(revisionID); err != nil {
			return "", ErrAdminResourceNotFound
		}
		result, err := tx.ExecContext(ctx, `UPDATE resource_revisions revision SET name=$3,summary=$4,paid_type=$5,publication_plan=$6,created_by=$7 WHERE revision.id=$1 AND revision.resource_id=$2 AND ((revision.state='draft' AND revision.created_via='admin') OR (revision.state='submitted' AND EXISTS(SELECT 1 FROM review_cases review WHERE review.revision_id=revision.id AND review.state='pending')))`, revisionID, resourceID, input.Name, input.Summary, input.PaidType, input.PublicationPlan, actor.UserID)
		if err != nil {
			return "", err
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return "", fmt.Errorf("%w: editable admin draft or pending review was not found", ErrAdminResourceConflict)
		}
	} else {
		if _, err := uuid.Parse(input.BaseRevisionID); err != nil {
			return "", fmt.Errorf("%w: base revision", ErrAdminResourceConflict)
		}
		var basePlan []byte
		if err := tx.QueryRowContext(ctx, `SELECT publication_plan FROM resource_revisions WHERE id=$1 AND resource_id=$2`, input.BaseRevisionID, resourceID).Scan(&basePlan); errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: base revision", ErrAdminResourceConflict)
		} else if err != nil {
			return "", err
		}
		var draftID string
		err := tx.QueryRowContext(ctx, `SELECT id::text FROM resource_revisions WHERE resource_id=$1 AND state='draft' AND created_via='admin' ORDER BY revision_no DESC LIMIT 1`, resourceID).Scan(&draftID)
		if err == nil {
			return "", fmt.Errorf("%w: draft %s already exists", ErrAdminResourceConflict, draftID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if string(input.PublicationPlan) == "[]" && len(basePlan) > 0 {
			input.PublicationPlan = append(json.RawMessage(nil), basePlan...)
		}
		var revisionNo int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM resource_revisions WHERE resource_id=$1`, resourceID).Scan(&revisionNo); err != nil {
			return "", err
		}
		revisionID = uuid.NewString()
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,paid_type,state,publication_plan,created_by,created_via,base_revision_id) VALUES($1,$2,$3,$4,$5,$6,'draft',$7,$8,'admin',$9)`, revisionID, resourceID, revisionNo, input.Name, input.Summary, input.PaidType, input.PublicationPlan, actor.UserID, input.BaseRevisionID); err != nil {
			return "", err
		}
		if err := cloneAdminRevisionAssets(ctx, tx, input.BaseRevisionID, revisionID); err != nil {
			return "", err
		}
		if err := cloneAdminRevisionGovernance(ctx, tx, input.BaseRevisionID, revisionID); err != nil {
			return "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM resource_revision_attributes WHERE revision_id=$1`, revisionID); err != nil {
		return "", err
	}
	for _, attribute := range input.Attributes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO resource_revision_attributes(revision_id,attribute) VALUES($1,$2)`, revisionID, attribute); err != nil {
			return "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM revision_links WHERE revision_id=$1`, revisionID); err != nil {
		return "", err
	}
	for _, link := range input.Links {
		if _, err := tx.ExecContext(ctx, `INSERT INTO revision_links(revision_id,position,title,url) VALUES($1,$2,$3,$4)`, revisionID, link.Position, link.Title, link.URL); err != nil {
			return "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resources SET draft_name=$2,updated_at=now() WHERE id=$1`, resourceID, input.Name); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"revision_id": revisionID, "base_revision_id": input.BaseRevisionID, "via": "admin"})
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_events(resource_id,actor_id,event_type,payload) VALUES($1,$2,'revision.admin_drafted',$3)`, resourceID, actor.UserID, payload); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return revisionID, nil
}

// AdminCreateRollbackRevision creates an editable management draft whose full
// snapshot is copied from an arbitrary historical revision. It deliberately
// remains a draft so it must pass through the normal submit and review flow.
func (s *Store) AdminCreateRollbackRevision(ctx context.Context, resourceID, baseRevisionID string, actor AdminSession) (string, error) {
	var input AdminRevisionDraftInput
	var attributesRaw, linksRaw []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision.name,revision.summary,revision.paid_type,revision.publication_plan,
		COALESCE((SELECT jsonb_agg(attribute ORDER BY attribute) FROM resource_revision_attributes WHERE revision_id=revision.id),'[]'::jsonb),
		COALESCE((SELECT jsonb_agg(jsonb_build_object('position',position,'title',title,'url',url) ORDER BY position) FROM revision_links WHERE revision_id=revision.id),'[]'::jsonb)
		FROM resource_revisions revision WHERE revision.id=$1 AND revision.resource_id=$2`, baseRevisionID, resourceID).
		Scan(&input.Name, &input.Summary, &input.PaidType, &input.PublicationPlan, &attributesRaw, &linksRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrAdminResourceNotFound
	}
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(attributesRaw, &input.Attributes); err != nil {
		return "", err
	}
	if err := json.Unmarshal(linksRaw, &input.Links); err != nil {
		return "", err
	}
	input.BaseRevisionID = baseRevisionID
	return s.AdminSaveRevisionDraft(ctx, resourceID, input, actor)
}

// AdminDiscardRevisionDraft removes only an editable admin draft. Submitted or
// historical revisions can never be discarded through this operation.
func (s *Store) AdminDiscardRevisionDraft(ctx context.Context, resourceID, revisionID string, actor AdminSession) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockAdminDraft(ctx, tx, resourceID, revisionID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM resource_revisions WHERE id=$1 AND resource_id=$2`, revisionID, resourceID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("%w: admin draft was not found", ErrAdminResourceConflict)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resources resource SET draft_name=COALESCE((SELECT name FROM resource_revisions WHERE id=resource.current_revision_id),'') ,updated_at=now() WHERE id=$1`, resourceID); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"revision_id": revisionID, "via": "admin"})
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_events(resource_id,actor_id,event_type,payload) VALUES($1,$2,'revision.admin_discarded',$3)`, resourceID, actor.UserID, payload); err != nil {
		return err
	}
	return tx.Commit()
}

func snapshotAdminRevisionGovernance(ctx context.Context, tx *sql.Tx, resourceID, revisionID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE resource_revisions revision SET governance_source=COALESCE((SELECT jsonb_build_object('author_name',source.author_name,'source_url',source.source_url,'license_name',source.license_name,'authorization_note',source.authorization_note) FROM resource_sources source WHERE source.resource_id=$1),'{}'::jsonb),governance_collection_id=resource.collection_id,governance_collection_position=resource.collection_position FROM resources resource WHERE revision.id=$2 AND resource.id=$1`, resourceID, revisionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO resource_revision_collaborators(revision_id,user_id) SELECT $2,user_id FROM resource_collaborators WHERE resource_id=$1 AND accepted_at IS NOT NULL`, resourceID, revisionID)
	return err
}

func cloneAdminRevisionGovernance(ctx context.Context, tx *sql.Tx, baseRevisionID, revisionID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE resource_revisions target SET governance_source=base.governance_source,governance_collection_id=base.governance_collection_id,governance_collection_position=base.governance_collection_position FROM resource_revisions base WHERE target.id=$2 AND base.id=$1`, baseRevisionID, revisionID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO resource_revision_collaborators(revision_id,user_id) SELECT $2,user_id FROM resource_revision_collaborators WHERE revision_id=$1`, baseRevisionID, revisionID)
	return err
}

func cloneAdminRevisionAssets(ctx context.Context, tx *sql.Tx, baseRevisionID, revisionID string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO revision_media(id,revision_id,blob_sha256,role,position,width,height) SELECT md5(random()::text||clock_timestamp()::text||id::text)::uuid,$2,blob_sha256,role,position,width,height FROM revision_media WHERE revision_id=$1`, baseRevisionID, revisionID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id::text,blob_sha256,original_name,package_format,package_id,package_version,analysis FROM revision_artifacts WHERE revision_id=$1 ORDER BY created_at,id`, baseRevisionID)
	if err != nil {
		return err
	}
	type clonedArtifact struct {
		oldID, newID, digest, name, format, packageID, version string
		analysis                                               []byte
	}
	artifacts := []clonedArtifact{}
	for rows.Next() {
		var oldID, digest, name, format, packageID, version string
		var analysis []byte
		if err := rows.Scan(&oldID, &digest, &name, &format, &packageID, &version, &analysis); err != nil {
			rows.Close()
			return err
		}
		newID := uuid.NewString()
		artifacts = append(artifacts, clonedArtifact{oldID: oldID, newID: newID, digest: digest, name: name, format: format, packageID: packageID, version: version, analysis: append([]byte(nil), analysis...)})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO revision_artifacts(id,revision_id,blob_sha256,original_name,package_format,package_id,package_version,analysis) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, artifact.newID, revisionID, artifact.digest, artifact.name, artifact.format, artifact.packageID, artifact.version, artifact.analysis); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO revision_artifact_devices(revision_id,artifact_id,device_id) SELECT $3,$2,device_id FROM revision_artifact_devices WHERE artifact_id=$1`, artifact.oldID, artifact.newID, revisionID); err != nil {
			return err
		}
	}
	return nil
}

// AdminSubmitRevisionDraft freezes an admin-authored draft as a submitted
// revision and creates the same review/publication work items as creator submit.
func (s *Store) AdminSubmitRevisionDraft(ctx context.Context, resourceID, revisionID string, actor AdminSession) error {
	if _, err := uuid.Parse(resourceID); err != nil {
		return ErrAdminResourceNotFound
	}
	if _, err := uuid.Parse(revisionID); err != nil {
		return fmt.Errorf("%w: draft revision", ErrAdminResourceConflict)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var name string
	var planRaw []byte
	if err := tx.QueryRowContext(ctx, `SELECT revision.name,revision.publication_plan FROM resource_revisions revision JOIN resources resource ON resource.id=revision.resource_id WHERE revision.id=$1 AND revision.resource_id=$2 AND revision.state='draft' AND revision.created_via='admin' FOR UPDATE OF revision,resource`, revisionID, resourceID).Scan(&name, &planRaw); errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: admin draft was not found", ErrAdminResourceConflict)
	} else if err != nil {
		return err
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%w: resource name is required", ErrAdminResourceConflict)
	}
	var previewCount, artifactCount, boundArtifactCount int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM revision_media WHERE revision_id=$1 AND role='preview'`, revisionID).Scan(&previewCount); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE EXISTS(SELECT 1 FROM revision_artifact_devices binding WHERE binding.artifact_id=artifact.id)) FROM revision_artifacts artifact WHERE artifact.revision_id=$1`, revisionID).Scan(&artifactCount, &boundArtifactCount); err != nil {
		return err
	}
	if previewCount < 1 || artifactCount < 1 || boundArtifactCount != artifactCount {
		return fmt.Errorf("%w: submission requires a preview and device-bound resource files", ErrAdminResourceConflict)
	}
	var plan []struct {
		Target string         `json:"target"`
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(planRaw, &plan); err != nil {
		return fmt.Errorf("%w: publication plan", ErrAdminResourceConflict)
	}
	seen := map[string]bool{}
	for _, publication := range plan {
		if seen[publication.Target] || (publication.Target != "oronbox" && publication.Target != "bandbbs" && publication.Target != "astrobox") {
			return fmt.Errorf("%w: publication plan", ErrAdminResourceConflict)
		}
		seen[publication.Target] = true
		if publication.Target != "oronbox" && len(publication.Config) == 0 {
			return fmt.Errorf("%w: %s publication settings are incomplete", ErrAdminResourceConflict, publication.Target)
		}
		if publication.Target == "astrobox" {
			var requiredMedia int
			if err := tx.QueryRowContext(ctx, `SELECT count(DISTINCT role) FROM revision_media WHERE revision_id=$1 AND role IN ('icon','cover')`, revisionID).Scan(&requiredMedia); err != nil {
				return err
			}
			if requiredMedia != 2 {
				return fmt.Errorf("%w: AstroBox requires an icon and a cover", ErrAdminResourceConflict)
			}
		}
	}
	if !seen["oronbox"] {
		plan = append(plan, struct {
			Target string         `json:"target"`
			Config map[string]any `json:"config"`
		}{Target: "oronbox", Config: map[string]any{}})
		planRaw, _ = json.Marshal(plan)
		if _, err := tx.ExecContext(ctx, `UPDATE resource_revisions SET publication_plan=$2 WHERE id=$1`, revisionID, planRaw); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_revisions SET state='superseded' WHERE resource_id=$1 AND state='submitted' AND id<>$2`, resourceID, revisionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE review_cases review SET state='superseded',updated_at=now() FROM resource_revisions revision WHERE review.revision_id=revision.id AND revision.resource_id=$1 AND revision.id<>$2 AND review.state='pending'`, resourceID, revisionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE publications publication SET state='cancelled',updated_at=now() FROM resource_revisions revision WHERE publication.revision_id=revision.id AND revision.resource_id=$1 AND revision.id<>$2 AND publication.state='pending'`, resourceID, revisionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_revisions SET state='submitted' WHERE id=$1`, revisionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO review_cases(id,revision_id) VALUES($1,$2)`, uuid.NewString(), revisionID); err != nil {
		return err
	}
	for _, publication := range plan {
		config, err := json.Marshal(publication.Config)
		if err != nil {
			return fmt.Errorf("%w: publication config", ErrAdminResourceConflict)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO publications(id,revision_id,target,config) VALUES($1,$2,$3,$4)`, uuid.NewString(), revisionID, publication.Target, config); err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]any{"revision_id": revisionID, "via": "admin"})
	if _, err := tx.ExecContext(ctx, `INSERT INTO resource_events(resource_id,actor_id,event_type,payload) VALUES($1,$2,'revision.admin_submitted',$3)`, resourceID, actor.UserID, payload); err != nil {
		return err
	}
	return tx.Commit()
}
