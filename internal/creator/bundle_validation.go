package creator

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/png"
	"math"
	"strings"

	resourcecore "github.com/zxor-org/OronBox-Server/internal/resource"
)

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

func (s *Service) verifyBundleMedia(files map[string]*zip.File, manifest *publishManifest, submit bool) ([]verifiedMedia, error) {
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
	if (submit && len(manifest.Media.Previews) < 1) || len(manifest.Media.Previews) > s.limits.PreviewMaxCount {
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

func (s *Service) verifyBundleArtifacts(files map[string]*zip.File, kind ResourceKind, refs []publishArtifactRef, required bool) ([]verifiedArtifact, error) {
	if required && len(refs) < 1 {
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
		if required && len(ref.DeviceIDs) < 1 {
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

func (s *Service) verifyBundlePublications(ctx context.Context, tx *sql.Tx, ownerID string, manifest *publishManifest, artifacts []verifiedArtifact, publications []PublicationRequest) error {
	for _, request := range publications {
		switch request.Target {
		case PublishOronBox:
		case PublishBandBBS:
			if !validBandBBSConfig(request.Config) {
				return fmt.Errorf("%w: BandBBS publication settings are incomplete", ErrInvalid)
			}
			if manifest.PurchaseLink != "" {
				if manifest.PurchasePrice == nil || math.IsNaN(*manifest.PurchasePrice) || math.IsInf(*manifest.PurchasePrice, 0) || *manifest.PurchasePrice <= 0 {
					return fmt.Errorf("%w: BandBBS external purchase amount", ErrInvalid)
				}
				if rawPrice, exists := request.Config["price"]; exists {
					price, ok := rawPrice.(float64)
					if !ok || math.IsNaN(price) || math.IsInf(price, 0) || math.Abs(price-*manifest.PurchasePrice) > 0.005 {
						return fmt.Errorf("%w: BandBBS external purchase amount does not match resource metadata", ErrInvalid)
					}
				}
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
