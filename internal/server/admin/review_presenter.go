package admin

import (
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func isPendingReview(rev store.AdminRevision) bool {
	return rev.State == "submitted" && rev.ReviewState == "pending"
}

func reviewItemJSON(item store.AdminReviewItem) map[string]any {
	due := ""
	if !item.DueAt.IsZero() {
		due = item.DueAt.Format(time.RFC3339)
	}
	first := ""
	if !item.FirstSubmittedAt.IsZero() {
		first = item.FirstSubmittedAt.Format(time.RFC3339)
	}
	return map[string]any{
		"id":                 item.ID,
		"state":              item.State,
		"name":               item.RevisionName,
		"curation_grade":     item.CurationGrade,
		"items":              item.Items,
		"owner":              item.Owner,
		"owner_id":           item.OwnerID,
		"kind":               item.ResourceKind,
		"slug":               item.ResourceSlug,
		"platform":           item.ResourcePlatform,
		"resource_state":     item.ResourceState,
		"revision_id":        item.RevisionID,
		"revision_number":    item.RevisionNumber,
		"resource_id":        item.ResourceID,
		"reviewer":           item.Reviewer,
		"reviewer_id":        item.ReviewerID,
		"targets":            item.Targets,
		"priority":           item.Priority,
		"priority_label":     item.PriorityLabel(),
		"overdue":            item.Overdue(),
		"due_at":             due,
		"waiting":            item.WaitingFor().Round(time.Minute).String(),
		"reports":            item.Reports,
		"owner_rejections":   item.OwnerRejections,
		"first_submitted_at": first,
		"created_at":         item.CreatedAt,
		"updated_at":         item.UpdatedAt,
	}
}

func revisionJSON(rev store.AdminReviewRevisionSnapshot) map[string]any {
	if rev.ID == "" {
		return nil
	}
	return map[string]any{
		"id":                 rev.ID,
		"number":             rev.Number,
		"name":               rev.Name,
		"summary":            rev.Summary,
		"description":        rev.Description,
		"description_format": rev.DescriptionFormat,
		"paid_type":          rev.PaidType,
		"state":              rev.State,
		"created_by":         rev.CreatedBy,
		"created_via":        rev.CreatedVia,
		"base_revision_id":   rev.BaseRevisionID,
		"publication_plan":   rev.PublicationPlan,
		"attributes":         rev.Attributes,
		"links":              rev.Links,
		"governance":         rev.Governance,
		"media":              mediaJSON(rev.Media),
		"artifacts":          artifactJSON(rev.Artifacts),
		"created_at":         rev.CreatedAt.Format(time.RFC3339),
	}
}

func mediaJSON(items []store.AdminMedia) []map[string]any {
	media := make([]map[string]any, 0, len(items))
	for _, mediaItem := range items {
		media = append(media, map[string]any{
			"id": mediaItem.ID, "sha256": mediaItem.SHA256, "role": mediaItem.Role, "position": mediaItem.Position,
			"width": mediaItem.Width, "height": mediaItem.Height, "size": mediaItem.SizeBytes,
			"url": "/admin/blobs/" + mediaItem.SHA256,
		})
	}
	return media
}

func artifactJSON(items []store.AdminArtifact) []map[string]any {
	artifacts := make([]map[string]any, 0, len(items))
	for _, artifact := range items {
		artifacts = append(artifacts, map[string]any{
			"id": artifact.ID, "name": artifact.OriginalName, "version": artifact.Version, "size": artifact.SizeBytes,
			"package_format": artifact.PackageFormat, "package_id": artifact.PackageID, "analysis": artifact.Analysis,
			"devices": artifact.Devices, "device_bindings": artifact.DeviceBindings, "sha256": artifact.SHA256,
			"url": "/admin/blobs/" + artifact.SHA256,
		})
	}
	return artifacts
}

func reviewEventsJSON(items []store.AdminReviewEvent) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id": item.ID, "event": item.Event, "actor": item.Actor, "note": item.Note,
			"checklist": item.Checklist, "detail": item.Detail, "created_at": item.CreatedAt,
		})
	}
	return out
}

func reviewChangeJSON(items []store.AdminReviewItemChange) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"key": item.Key, "label": item.Label, "change": item.Change, "before": item.Before, "after": item.After,
		})
	}
	return out
}

func diffJSON(diff store.AdminReviewDiff) map[string]any {
	fields := make([]map[string]any, 0, len(diff.Metadata))
	for _, field := range diff.Metadata {
		fields = append(fields, map[string]any{"label": field.Label, "before": field.Before, "after": field.After})
	}
	return map[string]any{
		"has_base":   diff.HasBase,
		"fields":     fields,
		"attributes": reviewChangeJSON(diff.AttributeItems),
		"links":      reviewChangeJSON(diff.LinkItems),
		"media":      reviewChangeJSON(diff.MediaItems),
		"artifacts":  reviewChangeJSON(diff.ArtifactItems),
		"devices":    reviewChangeJSON(diff.DeviceItems),
	}
}
