package creator

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

func (s *Service) saveBundle(ctx context.Context, ownerID, resourceID string, bundle []byte, submit bool, expected *int64) (Workspace, error) {
	reader, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: bundle is not a zip archive", ErrInvalid)
	}
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		if !file.FileInfo().IsDir() {
			files[file.Name] = file
		}
	}
	manifestRaw, err := readBundleFile(files, "manifest.json", 1<<20)
	if err != nil {
		return Workspace{}, err
	}
	var manifest publishManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return Workspace{}, fmt.Errorf("%w: manifest.json is not valid JSON", ErrInvalid)
	}
	manifest.Name = strings.TrimSpace(manifest.Name)
	manifest.Summary = strings.TrimSpace(manifest.Summary)
	if manifest.DescriptionFormat == "" {
		manifest.DescriptionFormat = "markdown"
	}
	if manifest.DescriptionFormat != "plain" && manifest.DescriptionFormat != "markdown" {
		return Workspace{}, fmt.Errorf("%w: description format", ErrInvalid)
	}
	if len([]rune(manifest.Description)) > 100000 {
		return Workspace{}, fmt.Errorf("%w: description too long", ErrInvalid)
	}
	manifest.PaidType = ResourcePaidType(strings.TrimSpace(string(manifest.PaidType)))
	manifest.PurchaseLink = strings.TrimSpace(manifest.PurchaseLink)
	if manifest.PaidType == "" {
		// Older clients did not send payment metadata; preserve their behavior.
		manifest.PaidType = ResourcePaidFree
	}
	if manifest.PurchaseLink != "" {
		parsed, parseErr := url.ParseRequestURI(manifest.PurchaseLink)
		if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || (publishesToAstroBox(manifest.Publications) && parsed.Scheme != "https") || len(manifest.PurchaseLink) > 2048 || manifest.PaidType == ResourcePaidFree {
			return Workspace{}, fmt.Errorf("%w: external purchase link", ErrInvalid)
		}
		manifest.PurchaseCurrency = "CNY"
		if (submit && manifest.PurchasePrice == nil) || (manifest.PurchasePrice != nil && !validPurchasePriceCNY(*manifest.PurchasePrice)) {
			return Workspace{}, fmt.Errorf("%w: external purchase amount", ErrInvalid)
		}
	} else {
		manifest.PurchasePrice = nil
		manifest.PurchaseCurrency = ""
	}
	kind := ResourceKind(strings.TrimSpace(manifest.Kind))
	if manifest.Version != 1 || !kind.Valid() || !manifest.PaidType.Valid() || manifest.Name == "" || (submit && manifest.Summary == "") || len(manifest.Name) > 120 || len(manifest.Summary) > 4000 {
		return Workspace{}, fmt.Errorf("%w: manifest metadata", ErrInvalid)
	}
	seenAttributes := make(map[string]bool, len(manifest.Attributes))
	for index, attribute := range manifest.Attributes {
		attribute = strings.TrimSpace(attribute)
		manifest.Attributes[index] = attribute
		if attribute == "" || seenAttributes[attribute] {
			return Workspace{}, fmt.Errorf("%w: resource attributes", ErrInvalid)
		}
		seenAttributes[attribute] = true
	}
	if len(seenAttributes) > 32 || !s.attributesExist(ctx, manifest.Attributes, true) {
		return Workspace{}, fmt.Errorf("%w: resource attributes", ErrInvalid)
	}
	if len(manifest.Links) > 16 {
		return Workspace{}, fmt.Errorf("%w: resource links", ErrInvalid)
	}
	for index := range manifest.Links {
		link := &manifest.Links[index]
		link.Title = strings.TrimSpace(link.Title)
		link.URL = strings.TrimSpace(link.URL)
		parsed, parseErr := url.ParseRequestURI(link.URL)
		if link.Title == "" || len(link.Title) > 80 || len(link.URL) > 2048 || parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return Workspace{}, fmt.Errorf("%w: resource links", ErrInvalid)
		}
	}
	media, err := s.verifyBundleMedia(files, &manifest, submit)
	if err != nil {
		return Workspace{}, err
	}
	artifacts, err := s.verifyBundleArtifacts(files, kind, manifest.Artifacts, submit)
	if err != nil {
		return Workspace{}, err
	}
	if err := s.mapPublications(ctx, kind, artifacts, manifest.Publications, submit); err != nil {
		return Workspace{}, err
	}
	if err := s.bindAstroBoxPlan(ctx, ownerID, resourceID, manifest.Publications); err != nil {
		return Workspace{}, err
	}
	publications := manifest.Publications
	seen := map[PublicationTarget]bool{}
	for _, request := range publications {
		if seen[request.Target] || (request.Target != PublishOronBox && request.Target != PublishBandBBS && request.Target != PublishAstroBox) {
			return Workspace{}, fmt.Errorf("%w: publication target", ErrInvalid)
		}
		seen[request.Target] = true
	}
	// The publish intent is stored on the revision for both draft and submit;
	// dispatchable publication rows are only created on submit.
	plan, err := json.Marshal(manifest.Publications)
	if err != nil {
		return Workspace{}, fmt.Errorf("%w: publication target", ErrInvalid)
	}
	if !submit {
		publications = nil
	}
	if submit && !seen[PublishOronBox] {
		publications = append(publications, PublicationRequest{Target: PublishOronBox, Config: map[string]any{}})
	}
	if manifest.Bindings == nil {
		var raw []byte
		err := s.db.QueryRowContext(ctx, `SELECT rr.binding_claims FROM resource_revisions rr JOIN resources r ON r.id=rr.resource_id WHERE r.id=$1 AND r.owner_id=$2 ORDER BY rr.revision_no DESC LIMIT 1`, resourceID, ownerID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Workspace{}, err
		}
		if len(raw) > 0 {
			if err = json.Unmarshal(raw, &manifest.Bindings); err != nil {
				return Workspace{}, err
			}
		}
	}
	if err := validateManifestBindings(manifest.Bindings); err != nil {
		return Workspace{}, err
	}

	if submit {
		rows, err := s.db.QueryContext(ctx, `SELECT b.provider,b.external_id,b.external_url,b.meta FROM external_bindings b JOIN resources r ON r.id=b.resource_id WHERE r.id=$1 AND r.owner_id=$2 AND b.origin='imported'`, resourceID, ownerID)
		if err != nil {
			return Workspace{}, err
		}
		present := map[string]bool{}
		for _, b := range manifest.Bindings {
			present[b.Provider] = true
		}
		for rows.Next() {
			var b ImportedBinding
			var meta []byte
			if err := rows.Scan(&b.Provider, &b.ExternalID, &b.ExternalURL, &meta); err != nil {
				rows.Close()
				return Workspace{}, err
			}
			if err := json.Unmarshal(meta, &b.Meta); err != nil {
				rows.Close()
				return Workspace{}, err
			}
			if !present[b.Provider] {
				manifest.Bindings = append(manifest.Bindings, b)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Workspace{}, err
		}
	}
	// Verify claims before opening a database transaction or reserving identity.
	if submit {
		for i, binding := range manifest.Bindings {
			if s.VerifyImportedBinding == nil {
				return Workspace{}, fmt.Errorf("%w: imported binding verifier unavailable", ErrInvalid)
			}
			verified, err := s.VerifyImportedBinding(ctx, ownerID, binding)
			if err != nil {
				return Workspace{}, fmt.Errorf("%w: %v", ErrInvalid, err)
			}
			manifest.Bindings[i] = verified
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()
	var currentEdit int64
	var baseRevisionID sql.NullString
	var resourceKind ResourceKind
	var moderationState string
	err = tx.QueryRowContext(ctx, `SELECT kind,moderation_state,edit_version,current_revision_id::text FROM resources WHERE id=$1 AND owner_id=$2 FOR UPDATE`, resourceID, ownerID).Scan(&resourceKind, &moderationState, &currentEdit, &baseRevisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	if expected != nil && currentEdit != *expected {
		return Workspace{}, fmt.Errorf("%w: revision_conflict", ErrConflict)
	}
	if moderationState == "frozen" {
		return Workspace{}, fmt.Errorf("%w: resource is frozen by an administrator", ErrConflict)
	}
	if moderationState == "deleted" {
		return Workspace{}, fmt.Errorf("%w: deleted resources cannot be published", ErrConflict)
	}
	if resourceKind != kind {
		return Workspace{}, fmt.Errorf("%w: manifest kind does not match the resource", ErrInvalid)
	}
	if err := s.verifyBundleBindings(ctx, tx, artifacts); err != nil {
		return Workspace{}, err
	}
	if submit {
		if err := s.saveManifestBindings(ctx, tx, resourceID, ownerID, manifest.Bindings); err != nil {
			return Workspace{}, err
		}
	}
	if submit {
		if err := s.verifyBundlePublications(ctx, tx, ownerID, &manifest, artifacts, publications); err != nil {
			return Workspace{}, err
		}
	}
	var savedDraftID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM resource_revisions WHERE resource_id=$1 AND state='draft' ORDER BY revision_no DESC LIMIT 1`, resourceID).Scan(&savedDraftID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM resource_revisions WHERE resource_id=$1 AND state='draft'`, resourceID); err != nil {
		return Workspace{}, err
	}
	stored := make(map[string]bool)
	put := func(payload []byte, mediaType string) (string, int64, error) {
		digest := sha256.Sum256(payload)
		key := hex.EncodeToString(digest[:])
		if stored[key] {
			return key, int64(len(payload)), nil
		}
		object, err := s.blobs.Put(ctx, bytes.NewReader(payload))
		if err != nil {
			return "", 0, err
		}
		if object.SHA256 != key {
			return "", 0, fmt.Errorf("blob store digest mismatch")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO blobs(sha256,size_bytes,media_type,local_key) VALUES($1,$2,$3,$4) ON CONFLICT(sha256) DO NOTHING`, object.SHA256, object.Size, mediaType, object.Key); err != nil {
			return "", 0, err
		}
		stored[key] = true
		return object.SHA256, object.Size, nil
	}

	var revisionNo int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(revision_no),0)+1 FROM resource_revisions WHERE resource_id=$1`, resourceID).Scan(&revisionNo); err != nil {
		return Workspace{}, err
	}
	revisionID := savedDraftID
	if revisionID == "" {
		revisionID = uuid.NewString()
	}
	revisionState := "draft"
	if submit {
		revisionState = "submitted"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,paid_type,purchase_link,purchase_price,purchase_currency,state,publication_plan,created_by,created_via,description,description_format,base_revision_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'creator',$13,$14,$15)`, revisionID, resourceID, revisionNo, manifest.Name, manifest.Summary, manifest.PaidType, manifest.PurchaseLink, manifest.PurchasePrice, manifest.PurchaseCurrency, revisionState, plan, ownerID, manifest.Description, manifest.DescriptionFormat, baseRevisionID); err != nil {
		return Workspace{}, err
	}
	claims := manifest.Bindings
	if claims == nil {
		claims = []ImportedBinding{}
	}
	rawClaims, _ := json.Marshal(claims)
	if _, err = tx.ExecContext(ctx, `UPDATE resource_revisions SET binding_claims=$2 WHERE id=$1`, revisionID, rawClaims); err != nil {
		return Workspace{}, err
	}
	for attribute := range seenAttributes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO resource_revision_attributes(revision_id,attribute) VALUES($1,$2)`, revisionID, attribute); err != nil {
			return Workspace{}, err
		}
	}
	for position, link := range manifest.Links {
		if _, err = tx.ExecContext(ctx, `INSERT INTO revision_links(revision_id,position,title,url) VALUES($1,$2,$3,$4)`, revisionID, position, link.Title, link.URL); err != nil {
			return Workspace{}, err
		}
	}
	for _, item := range media {
		digest, _, err := put(item.payload, item.mediaType)
		if err != nil {
			return Workspace{}, err
		}
		if digest != item.sha256 {
			return Workspace{}, fmt.Errorf("%w: media digest changed during storage", ErrInvalid)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO revision_media(id,revision_id,blob_sha256,role,position,width,height) VALUES($1,$2,$3,$4,$5,$6,$7)`, uuid.NewString(), revisionID, digest, item.role, item.position, item.width, item.height); err != nil {
			return Workspace{}, err
		}
	}
	for _, artifact := range artifacts {
		digest, _, err := put(artifact.payload, "application/octet-stream")
		if err != nil {
			return Workspace{}, err
		}
		analysisJSON, _ := json.Marshal(artifact.analysis)
		artifactID := uuid.NewString()
		if _, err = tx.ExecContext(ctx, `INSERT INTO revision_artifacts(id,revision_id,blob_sha256,original_name,package_format,package_id,package_version,analysis) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, artifactID, revisionID, digest, artifact.originalName, artifact.analysis.PackageFormat, artifact.analysis.PackageID, artifact.analysis.Version, analysisJSON); err != nil {
			return Workspace{}, err
		}
		for _, deviceID := range unique(artifact.deviceIDs) {
			if _, err = tx.ExecContext(ctx, `INSERT INTO revision_artifact_devices(revision_id,artifact_id,device_id) VALUES($1,$2,$3)`, revisionID, artifactID, deviceID); err != nil {
				return Workspace{}, err
			}
		}
	}
	if submit {
		if _, err = tx.ExecContext(ctx, `UPDATE resource_revisions SET state='superseded' WHERE resource_id=$1 AND state='submitted' AND id<>$2`, resourceID, revisionID); err != nil {
			return Workspace{}, err
		}
	}
	if submit {
		if _, err = tx.ExecContext(ctx, `UPDATE review_cases c SET state='superseded',updated_at=now() FROM resource_revisions rr WHERE c.revision_id=rr.id AND rr.resource_id=$1 AND rr.id<>$2 AND c.state='pending'`, resourceID, revisionID); err != nil {
			return Workspace{}, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE publications p SET state='cancelled',updated_at=now() FROM resource_revisions rr WHERE p.revision_id=rr.id AND rr.resource_id=$1 AND rr.id<>$2 AND p.state IN ('pending','running')`, resourceID, revisionID); err != nil {
			return Workspace{}, err
		}
	}
	if submit {
		if _, err = tx.ExecContext(ctx, `INSERT INTO review_cases(id,revision_id,first_submitted_at)
SELECT $1,$2,COALESCE((SELECT min(prior.first_submitted_at) FROM review_cases prior JOIN resource_revisions prior_revision ON prior_revision.id=prior.revision_id WHERE prior_revision.resource_id=revision.resource_id),now())
FROM resource_revisions revision WHERE revision.id=$2`, uuid.NewString(), revisionID); err != nil {
			return Workspace{}, err
		}
	}
	for _, request := range publications {
		config, err := json.Marshal(request.Config)
		if err != nil {
			return Workspace{}, fmt.Errorf("%w: publication config", ErrInvalid)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO publications(id,revision_id,target,config) VALUES($1,$2,$3,$4)`, uuid.NewString(), revisionID, request.Target, config); err != nil {
			return Workspace{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resources SET draft_name=$2,updated_at=now(),edit_version=edit_version+1 WHERE id=$1`, resourceID, manifest.Name); err != nil {
		return Workspace{}, err
	}
	eventName := "revision.drafted"
	if submit {
		eventName = "revision.published"
	}
	if err = event(ctx, tx, resourceID, ownerID, eventName, map[string]any{"revision_id": revisionID, "revision_no": revisionNo}); err != nil {
		return Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workspace{}, err
	}
	targets := make([]string, 0, len(publications))
	for _, request := range publications {
		targets = append(targets, string(request.Target))
	}
	log(ctx).Info("revision saved", "resource_id", resourceID, "owner_id", ownerID, "kind", kind, "revision_id", revisionID, "revision_no", revisionNo, "state", revisionState, "artifacts", len(artifacts), "targets", strings.Join(targets, ","))
	return s.Workspace(ctx, ownerID, resourceID)
}
