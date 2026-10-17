package resources

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

const adminRevisionArtifactsSQL = `SELECT artifact.id::text,artifact.blob_sha256,artifact.original_name,artifact.package_format,artifact.package_id,artifact.package_version,
 blob.size_bytes,artifact.analysis,
 COALESCE((SELECT jsonb_agg(device.display_name ORDER BY device.display_name) FROM revision_artifact_devices binding JOIN devices device ON device.id=binding.device_id WHERE binding.artifact_id=artifact.id),'[]'::jsonb),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',device.id::text,'display_name',device.display_name,'codename',device.codename,'platform',device.platform) ORDER BY device.display_name) FROM revision_artifact_devices binding JOIN devices device ON device.id=binding.device_id WHERE binding.artifact_id=artifact.id),'[]'::jsonb)
FROM revision_artifacts artifact JOIN blobs blob ON blob.sha256=artifact.blob_sha256 WHERE artifact.revision_id=$1 ORDER BY artifact.created_at`

const adminRevisionMediaSQL = `SELECT media.id::text,media.blob_sha256,media.role,media.position,media.width,media.height,blob.size_bytes FROM revision_media media JOIN blobs blob ON blob.sha256=media.blob_sha256 WHERE media.revision_id=$1 ORDER BY media.role,media.position`

func (s *Repository) adminArtifacts(ctx context.Context, query, snapshotID string) ([]AdminArtifact, error) {
	rows, err := s.db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	artifacts := []AdminArtifact{}
	byContent := make(map[string]int)
	for rows.Next() {
		var artifact AdminArtifact
		var analysis, devices, deviceBindings []byte
		if err := rows.Scan(&artifact.ID, &artifact.SHA256, &artifact.OriginalName, &artifact.PackageFormat, &artifact.PackageID, &artifact.Version, &artifact.SizeBytes, &analysis, &devices, &deviceBindings); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(analysis, &artifact.Analysis)
		_ = json.Unmarshal(devices, &artifact.Devices)
		_ = json.Unmarshal(deviceBindings, &artifact.DeviceBindings)
		artifact.DuplicateCount = 1
		key := strings.Join([]string{artifact.SHA256, artifact.OriginalName, artifact.PackageFormat, artifact.PackageID, artifact.Version}, "\x00")
		if index, ok := byContent[key]; ok {
			artifacts[index].DuplicateCount++
			artifacts[index].Devices = mergeStringValues(artifacts[index].Devices, artifact.Devices)
			artifacts[index].DeviceBindings = mergeArtifactDevices(artifacts[index].DeviceBindings, artifact.DeviceBindings)
			continue
		}
		byContent[key] = len(artifacts)
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

func mergeStringValues(left, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	merged := make([]string, 0, len(left)+len(right))
	for _, values := range [][]string{left, right} {
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				merged = append(merged, value)
			}
		}
	}
	sort.Strings(merged)
	return merged
}

func mergeArtifactDevices(left, right []AdminArtifactDevice) []AdminArtifactDevice {
	byID := make(map[string]AdminArtifactDevice, len(left)+len(right))
	for _, device := range append(left, right...) {
		byID[device.ID] = device
	}
	merged := make([]AdminArtifactDevice, 0, len(byID))
	for _, device := range byID {
		merged = append(merged, device)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].DisplayName < merged[j].DisplayName })
	return merged
}

func (s *Repository) adminMedia(ctx context.Context, query, snapshotID string) ([]AdminMedia, error) {
	rows, err := s.db.QueryContext(ctx, query, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	media := []AdminMedia{}
	for rows.Next() {
		var item AdminMedia
		if err := rows.Scan(&item.ID, &item.SHA256, &item.Role, &item.Position, &item.Width, &item.Height, &item.SizeBytes); err != nil {
			return nil, err
		}
		media = append(media, item)
	}
	return media, rows.Err()
}

func (s *Repository) RevisionArtifacts(ctx context.Context, id string) ([]AdminArtifact, error) {
	return s.adminArtifacts(ctx, adminRevisionArtifactsSQL, id)
}
func (s *Repository) RevisionMedia(ctx context.Context, id string) ([]AdminMedia, error) {
	return s.adminMedia(ctx, adminRevisionMediaSQL, id)
}
