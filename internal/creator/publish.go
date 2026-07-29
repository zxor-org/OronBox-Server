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
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"strings"

	"github.com/google/uuid"
	resourcecore "github.com/zxor-org/OronBox-Server/internal/resource"
)

const mediaMaxDimension = 1500

// Publish bundle layout: manifest.json plus the payload files it references.
// Media files are pre-processed by the client (resized, encoded); the server
// only verifies them. Artifact payloads are re-analyzed server-side.
type publishMediaRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type publishArtifactRef struct {
	File         string   `json:"file"`
	OriginalName string   `json:"original_name"`
	Type         string   `json:"type"`
	PackageID    string   `json:"package_id"`
	Version      string   `json:"package_version"`
	SHA256       string   `json:"sha256"`
	DeviceIDs    []string `json:"device_ids"`
}

type publishManifest struct {
	Version    int            `json:"version"`
	Kind       string         `json:"kind"`
	Name       string         `json:"name"`
	Summary    string         `json:"summary"`
	Attributes []string       `json:"attributes"`
	Links      []ResourceLink `json:"links"`
	Media      struct {
		Icon     *publishMediaRef  `json:"icon"`
		Cover    *publishMediaRef  `json:"cover"`
		Previews []publishMediaRef `json:"previews"`
	} `json:"media"`
	Artifacts    []publishArtifactRef `json:"artifacts"`
	Publications []PublicationRequest `json:"publications"`
}

type verifiedMedia struct {
	role      string
	position  int
	width     int
	height    int
	mediaType string
	sha256    string
	size      int64
	payload   []byte
}

type verifiedArtifact struct {
	originalName string
	analysis     resourcecore.Analysis
	sha256       string
	size         int64
	payload      []byte
	deviceIDs    []string
}

func (s *Service) Publish(ctx context.Context, ownerID, resourceID string, bundle []byte) (Workspace, error) {
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
	kind := ResourceKind(strings.TrimSpace(manifest.Kind))
	if manifest.Version != 1 || !kind.Valid() || manifest.Name == "" || len(manifest.Name) > 120 || len(manifest.Summary) > 4000 {
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
	media, err := s.verifyBundleMedia(files, &manifest)
	if err != nil {
		return Workspace{}, err
	}
	artifacts, err := s.verifyBundleArtifacts(files, kind, manifest.Artifacts)
	if err != nil {
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
	if !seen[PublishOronBox] {
		publications = append(publications, PublicationRequest{Target: PublishOronBox, Config: map[string]any{}})
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Workspace{}, err
	}
	defer tx.Rollback()
	var resourceKind ResourceKind
	var moderationState string
	err = tx.QueryRowContext(ctx, `SELECT kind,moderation_state FROM resources WHERE id=$1 AND owner_id=$2 FOR UPDATE`, resourceID, ownerID).Scan(&resourceKind, &moderationState)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	if moderationState == "frozen" {
		return Workspace{}, fmt.Errorf("%w: resource is frozen by an administrator", ErrConflict)
	}
	if resourceKind != kind {
		return Workspace{}, fmt.Errorf("%w: manifest kind does not match the resource", ErrInvalid)
	}
	if err := s.verifyBundleBindings(ctx, tx, artifacts); err != nil {
		return Workspace{}, err
	}
	if err := s.verifyBundlePublications(ctx, tx, ownerID, &manifest, artifacts, publications); err != nil {
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
	revisionID := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary) VALUES($1,$2,$3,$4,$5)`, revisionID, resourceID, revisionNo, manifest.Name, manifest.Summary); err != nil {
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
	if _, err = tx.ExecContext(ctx, `UPDATE resource_revisions SET state='superseded' WHERE resource_id=$1 AND state='submitted' AND id<>$2`, resourceID, revisionID); err != nil {
		return Workspace{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE review_cases c SET state='superseded',updated_at=now() FROM resource_revisions rr WHERE c.revision_id=rr.id AND rr.resource_id=$1 AND rr.id<>$2 AND c.state='pending'`, resourceID, revisionID); err != nil {
		return Workspace{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE publications p SET state='cancelled',updated_at=now() FROM resource_revisions rr WHERE p.revision_id=rr.id AND rr.resource_id=$1 AND rr.id<>$2 AND p.state IN ('pending','running')`, resourceID, revisionID); err != nil {
		return Workspace{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO review_cases(id,revision_id) VALUES($1,$2)`, uuid.NewString(), revisionID); err != nil {
		return Workspace{}, err
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
	if _, err = tx.ExecContext(ctx, `UPDATE resources SET updated_at=now() WHERE id=$1`, resourceID); err != nil {
		return Workspace{}, err
	}
	if err = event(ctx, tx, resourceID, ownerID, "revision.published", map[string]any{"revision_id": revisionID, "revision_no": revisionNo}); err != nil {
		return Workspace{}, err
	}
	if err = tx.Commit(); err != nil {
		return Workspace{}, err
	}
	targets := make([]string, 0, len(publications))
	for _, request := range publications {
		targets = append(targets, string(request.Target))
	}
	log(ctx).Info("revision published", "resource_id", resourceID, "owner_id", ownerID, "kind", kind, "revision_id", revisionID, "revision_no", revisionNo, "artifacts", len(artifacts), "targets", strings.Join(targets, ","))
	return s.Workspace(ctx, ownerID, resourceID)
}

func readBundleFile(files map[string]*zip.File, name string, limit int64) ([]byte, error) {
	file, ok := files[name]
	if !ok {
		return nil, fmt.Errorf("%w: bundle is missing %s", ErrInvalid, name)
	}
	if int64(file.UncompressedSize64) > limit {
		return nil, fmt.Errorf("%w: %s exceeds its size limit", ErrInvalid, name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	payload := make([]byte, 0, file.UncompressedSize64)
	buffer := bytes.NewBuffer(payload)
	if _, err := buffer.ReadFrom(reader); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Service) verifyBundleMedia(files map[string]*zip.File, manifest *publishManifest) ([]verifiedMedia, error) {
	type entry struct {
		role     string
		position int
		ref      *publishMediaRef
	}
	entries := []entry{}
	if manifest.Media.Icon != nil {
		entries = append(entries, entry{role: "icon", ref: manifest.Media.Icon})
	}
	if manifest.Media.Cover != nil {
		entries = append(entries, entry{role: "cover", ref: manifest.Media.Cover})
	}
	if len(manifest.Media.Previews) < 1 || len(manifest.Media.Previews) > s.limits.PreviewMaxCount {
		return nil, fmt.Errorf("%w: preview count", ErrInvalid)
	}
	for index := range manifest.Media.Previews {
		entries = append(entries, entry{role: "preview", position: index, ref: &manifest.Media.Previews[index]})
	}
	result := make([]verifiedMedia, 0, len(entries))
	for _, item := range entries {
		ref := item.ref
		payload, err := readBundleFile(files, ref.File, s.limits.PreviewMaxBytes+1)
		if err != nil {
			return nil, err
		}
		if err := verifySHA256(payload, ref.SHA256); err != nil {
			return nil, err
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("%w: %s is not a decodable image", ErrInvalid, ref.File)
		}
		if config.Width != ref.Width || config.Height != ref.Height || config.Width > mediaMaxDimension || config.Height > mediaMaxDimension || config.Width <= 0 || config.Height <= 0 {
			return nil, fmt.Errorf("%w: %s dimensions", ErrInvalid, ref.File)
		}
		result = append(result, verifiedMedia{
			role:      item.role,
			position:  item.position,
			width:     config.Width,
			height:    config.Height,
			mediaType: "image/" + format,
			sha256:    ref.SHA256,
			size:      int64(len(payload)),
			payload:   payload,
		})
	}
	return result, nil
}

func (s *Service) verifyBundleArtifacts(files map[string]*zip.File, kind ResourceKind, refs []publishArtifactRef) ([]verifiedArtifact, error) {
	if len(refs) < 1 {
		return nil, fmt.Errorf("%w: at least one resource file", ErrInvalid)
	}
	result := make([]verifiedArtifact, 0, len(refs))
	for _, ref := range refs {
		payload, err := readBundleFile(files, ref.File, s.limits.UploadMaxBytes+1)
		if err != nil {
			return nil, err
		}
		if err := verifySHA256(payload, ref.SHA256); err != nil {
			return nil, err
		}
		analysis, err := resourcecore.Analyze(payload)
		if err != nil || analysis.Platform != resourcecore.VelaOS || (analysis.Kind != resourcecore.QuickApp && analysis.Kind != resourcecore.Watchface) {
			return nil, fmt.Errorf("%w: %s is not a VelaOS quick app or watchface", ErrInvalid, ref.File)
		}
		declaredType := "velaos_quickapp"
		if analysis.Kind == resourcecore.Watchface {
			declaredType = "velaos_watchface"
		}
		if ref.Type != declaredType {
			return nil, fmt.Errorf("%w: %s type declaration does not match its payload", ErrInvalid, ref.File)
		}
		if (kind == QuickApp) != (analysis.Kind == resourcecore.QuickApp) {
			return nil, fmt.Errorf("%w: %s does not match the resource kind", ErrInvalid, ref.File)
		}
		if len(ref.DeviceIDs) < 1 {
			return nil, fmt.Errorf("%w: %s has no bound devices", ErrInvalid, ref.File)
		}
		result = append(result, verifiedArtifact{
			originalName: strings.TrimSpace(ref.OriginalName),
			analysis:     analysis,
			sha256:       ref.SHA256,
			size:         int64(len(payload)),
			payload:      analysis.Payload,
			deviceIDs:    ref.DeviceIDs,
		})
	}
	return result, nil
}

func (s *Service) verifyBundleBindings(ctx context.Context, tx *sql.Tx, artifacts []verifiedArtifact) error {
	claimed := map[string]bool{}
	for _, artifact := range artifacts {
		for _, deviceID := range unique(artifact.deviceIDs) {
			if claimed[deviceID] {
				return fmt.Errorf("%w: device %s is bound to more than one resource file", ErrInvalid, deviceID)
			}
			claimed[deviceID] = true
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE id=$1 AND platform='vela_os' AND codename NOT IN ('m66','n69'))`, deviceID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: unknown VelaOS device %s", ErrInvalid, deviceID)
			}
		}
	}
	return nil
}

func (s *Service) verifyBundlePublications(ctx context.Context, tx *sql.Tx, ownerID string, manifest *publishManifest, artifacts []verifiedArtifact, publications []PublicationRequest) error {
	for _, request := range publications {
		switch request.Target {
		case PublishOronBox:
		case PublishBandBBS:
			if !validBandBBSConfig(request.Config) {
				return fmt.Errorf("%w: BandBBS publication settings are incomplete", ErrInvalid)
			}
			packages := map[string]bool{}
			for _, artifact := range artifacts {
				packages[artifact.analysis.PackageID] = true
			}
			for _, packageID := range bandBBSTargetPackages(request.Config) {
				if !packages[packageID] {
					return fmt.Errorf("%w: BandBBS target package %s is not among the resource files", ErrInvalid, packageID)
				}
			}
			var authorized bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM oauth_grants WHERE user_id=$1 AND provider='bandbbs_publish' AND scopes@>ARRAY['resource:write'])`, ownerID).Scan(&authorized); err != nil {
				return err
			}
			if !authorized {
				return fmt.Errorf("%w: BandBBS publishing permission is required", ErrInvalid)
			}
		case PublishAstroBox:
			if !validAstroBoxConfig(request.Config) {
				return fmt.Errorf("%w: AstroBox publication settings are incomplete", ErrInvalid)
			}
			if manifest.Media.Icon == nil || manifest.Media.Cover == nil {
				return fmt.Errorf("%w: AstroBox requires an icon and a cover", ErrInvalid)
			}
			if fmt.Sprint(request.Config["mode"]) == "own" {
				var authorized bool
				if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM github_grants WHERE user_id=$1 AND scopes@>ARRAY['public_repo'])`, ownerID).Scan(&authorized); err != nil {
					return err
				}
				if !authorized {
					return fmt.Errorf("%w: GitHub publishing permission is required", ErrInvalid)
				}
			}
		}
	}
	return nil
}

func verifySHA256(payload []byte, declared string) error {
	digest := sha256.Sum256(payload)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), strings.TrimSpace(declared)) {
		return fmt.Errorf("%w: sha256 mismatch", ErrInvalid)
	}
	return nil
}
