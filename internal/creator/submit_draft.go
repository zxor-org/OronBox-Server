package creator

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/zxor-org/OronBox-Server/internal/blob"
	"io"
)

// Submit the exact saved draft. Reuse validation rather than allowing a second
// client-supplied payload to silently differ from the version shown to review.
func (s *Service) SubmitDraftAt(ctx context.Context, uid, rid, revisionID string, expected int64) (Workspace, error) {
	w, err := s.Workspace(ctx, uid, rid)
	if err != nil {
		return w, err
	}
	if len(w.Revisions) == 0 || w.Revisions[0].ID != revisionID {
		return Workspace{}, fmt.Errorf("%w: revision_conflict", ErrConflict)
	}
	revision := w.Revisions[0]
	if revision.State != RevisionDraft {
		if revision.State == RevisionSubmitted || revision.State == RevisionApproved {
			return w, nil
		}
		return Workspace{}, fmt.Errorf("%w: revision is not an editable draft", ErrConflict)
	}
	if w.Resource.EditVersion != expected {
		return Workspace{}, fmt.Errorf("%w: revision_conflict", ErrConflict)
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	var total int64
	appendBlob := func(path, digest string) error {
		file, err := s.blobs.Open(ctx, blob.SHA256Key(digest))
		if err != nil {
			return err
		}
		defer file.Close()
		out, err := writer.Create(path)
		if err != nil {
			return err
		}
		size, err := io.Copy(out, io.LimitReader(file, s.limits.UploadMaxBytes+1))
		if err != nil {
			return err
		}
		total += size
		if total > s.limits.UploadMaxBytes {
			return fmt.Errorf("%w: saved blob exceeds limit", ErrInvalid)
		}
		return nil
	}
	manifest := map[string]any{"version": 1, "kind": string(w.Resource.Kind), "name": revision.Name, "summary": revision.Summary, "description": revision.Description, "description_format": revision.DescriptionFormat, "paid_type": revision.PaidType, "purchase_link": revision.PurchaseLink, "purchase_price": revision.PurchasePrice, "attributes": revision.Attributes, "links": w.Links, "publications": revision.PublicationPlan, "bindings": revision.BindingClaims}
	media := map[string]any{}
	previews := []any{}
	for i, m := range w.Media {
		path := fmt.Sprintf("media/%d", i)
		if err := appendBlob(path, m.BlobSHA256); err != nil {
			return Workspace{}, err
		}
		ref := map[string]any{"file": path, "sha256": m.BlobSHA256, "width": m.Width, "height": m.Height}
		if m.Role == MediaPreview {
			previews = append(previews, ref)
		} else {
			media[string(m.Role)] = ref
		}
	}
	media["previews"] = previews
	manifest["media"] = media
	artifacts := []any{}
	for i, a := range w.Artifacts {
		path := fmt.Sprintf("artifacts/%d", i)
		if err := appendBlob(path, a.BlobSHA256); err != nil {
			return Workspace{}, err
		}
		kind := "velaos_quickapp"
		if w.Resource.Kind == Watchface {
			kind = "velaos_watchface"
		}
		artifacts = append(artifacts, map[string]any{"file": path, "original_name": a.OriginalName, "type": kind, "package_id": a.PackageID, "package_version": a.Version, "sha256": a.BlobSHA256, "device_ids": a.DeviceIDs})
	}
	manifest["artifacts"] = artifacts
	out, err := writer.Create("manifest.json")
	if err != nil {
		return Workspace{}, err
	}
	if err := json.NewEncoder(out).Encode(manifest); err != nil {
		return Workspace{}, err
	}
	if err := writer.Close(); err != nil {
		return Workspace{}, err
	}
	return s.PublishAt(ctx, uid, rid, buf.Bytes(), expected)
}
