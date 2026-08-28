package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/console"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) serveAdminSPA(w http.ResponseWriter, r *http.Request) {
	root, err := fs.Sub(console.Dist, "dist")
	if err != nil {
		http.Error(w, "console is not available", http.StatusInternalServerError)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/admin/")
	if path == "" || path == "/" || !strings.Contains(path, ".") {
		path = "index.html"
	}
	file, err := root.Open(path)
	if err != nil {
		file, err = root.Open("index.html")
		if err != nil {
			http.Error(w, "console is not built", http.StatusInternalServerError)
			return
		}
		path = "index.html"
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reader, ok := file.(io.ReadSeeker)
	if !ok {
		http.Error(w, "console file is not readable", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, path, stat.ModTime(), reader)
}

func (a *App) handleAdminAPISession(w http.ResponseWriter, r *http.Request) {
	session := currentAdmin(r)
	role, _ := r.Context().Value(adminRoleContextKey{}).(string)
	pending, err := a.store.AdminReviews(r.Context(), store.AdminReviewQuery{State: "pending", Page: 1, PerPage: 1})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("session_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":            session.Username,
		"role":            role,
		"csrf_token":      a.adminCSRFTokenFor(r),
		"pending_reviews": pending.Total,
	})
}

func (a *App) handleAdminAPIReviews(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminReviews(r.Context(), store.AdminReviewQuery{
		Search:  r.URL.Query().Get("q"),
		Kind:    r.URL.Query().Get("kind"),
		Target:  r.URL.Query().Get("target"),
		Owner:   r.URL.Query().Get("owner"),
		State:   r.URL.Query().Get("state"),
		From:    from,
		To:      to,
		Sort:    r.URL.Query().Get("sort"),
		Page:    positiveInt(r.URL.Query().Get("page"), 1),
		PerPage: positiveInt(r.URL.Query().Get("per_page"), 40),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("reviews_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, reviewItemJSON(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": page.Total, "page": page.Page, "per_page": page.PerPage,
	})
}

func (a *App) handleAdminAPIReview(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminReview(r.Context(), r.PathValue("review"))
	if errors.Is(err, store.ErrAdminReviewNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "review case was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("review_failed", err.Error()))
		return
	}
	events, _ := a.store.AdminReviewEvents(r.Context(), detail.Review.ID)
	reviewers, _ := a.store.AdminReviewers(r.Context())
	attributes, _ := a.creator.Attributes(r.Context(), false)
	devices, _ := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Page: 1, PerPage: 200})
	writeJSON(w, http.StatusOK, map[string]any{
		"review":            reviewItemJSON(detail.Review),
		"current":           revisionJSON(detail.Current),
		"base":              revisionJSON(detail.Base),
		"diff":              diffJSON(detail.Diff),
		"events":            reviewEventsJSON(events),
		"reviewers":         reviewers,
		"attributes":        attributes,
		"devices":           devices.Items,
		"checklist_catalog": defaultReviewChecklist,
	})
}

func (a *App) handleAdminAPIReviewDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision   string   `json:"decision"`
		Note       string   `json:"note"`
		Grade      string   `json:"grade"`
		Items      []string `json:"items"`
		Attributes []string `json:"attributes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if body.Decision != "approve" && body.Decision != "reject" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "decision must be approve or reject"))
		return
	}
	detail, err := a.store.AdminReview(r.Context(), r.PathValue("review"))
	if errors.Is(err, store.ErrAdminReviewNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "review case was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("review_failed", err.Error()))
		return
	}
	note := strings.TrimSpace(body.Note)
	approved := body.Decision == "approve"
	if !approved && note == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "退回必须填写理由"))
		return
	}
	grade := strings.TrimSpace(body.Grade)
	if grade != "featured" && grade != "standard" {
		grade = detail.Review.CurationGrade
	}
	if grade != "featured" {
		grade = "standard"
	}
	items := body.Items
	if len(items) == 0 {
		items = detail.Review.Items
	}
	attributes := body.Attributes
	if len(attributes) == 0 {
		attributes = detail.Current.Attributes
	}
	actor := currentAdmin(r)
	if err := a.creator.Review(r.Context(), detail.Current.ID, actor.UserID, approved, note, items, attributes, grade); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusConflict, errorBody("review_failed", err.Error()))
		return
	}
	result := "rejected"
	if approved {
		result = "approved"
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.review", result, a.clientIP(r), r.UserAgent(), "revision="+detail.Current.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIResources(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminResources(r.Context(), store.AdminResourceQuery{
		Search:            r.URL.Query().Get("q"),
		Kind:              r.URL.Query().Get("kind"),
		Owner:             r.URL.Query().Get("owner"),
		Moderation:        r.URL.Query().Get("state"),
		PublicationTarget: r.URL.Query().Get("target"),
		Sort:              r.URL.Query().Get("sort"),
		Page:              positiveInt(r.URL.Query().Get("page"), 1),
		PerPage:           positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("resources_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id":               item.ID,
			"name":             item.Name,
			"slug":             item.Slug,
			"kind":             item.Kind,
			"platform":         item.Platform,
			"owner":            item.Owner,
			"moderation":       item.ModerationState,
			"moderation_reason": item.ModerationReason,
			"revision_name":    item.CurrentRevisionName,
			"revision_number":  item.CurrentRevisionNumber,
			"revision_state":   item.CurrentRevisionState,
			"review_state":     item.LatestReviewState,
			"targets":          item.Targets,
			"updated_at":       item.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": page.Total, "page": page.Page, "per_page": page.PerPage,
	})
}

func (a *App) handleAdminAPIComments(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminComments(r.Context(), store.AdminCommentQuery{
		Search:  r.URL.Query().Get("q"),
		State:   r.URL.Query().Get("state"),
		Page:    positiveInt(r.URL.Query().Get("page"), 1),
		PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("comments_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id":          item.ID,
			"body":        item.Body,
			"username":    item.Username,
			"resource_id": item.ResourceID,
			"state":       item.ModerationState,
			"created_at":  item.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": page.Total, "page": page.Page, "per_page": page.PerPage,
	})
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
		"id":               item.ID,
		"state":            item.State,
		"name":             item.RevisionName,
		"curation_grade":   item.CurationGrade,
		"items":            item.Items,
		"owner":            item.Owner,
		"owner_id":         item.OwnerID,
		"kind":             item.ResourceKind,
		"slug":             item.ResourceSlug,
		"platform":         item.ResourcePlatform,
		"resource_state":   item.ResourceState,
		"revision_id":      item.RevisionID,
		"revision_number":  item.RevisionNumber,
		"resource_id":      item.ResourceID,
		"reviewer":         item.Reviewer,
		"reviewer_id":      item.ReviewerID,
		"targets":          item.Targets,
		"priority":         item.Priority,
		"priority_label":   item.PriorityLabel(),
		"overdue":          item.Overdue(),
		"due_at":           due,
		"waiting":          item.WaitingFor().Round(time.Minute).String(),
		"reports":          item.Reports,
		"owner_rejections": item.OwnerRejections,
		"first_submitted_at": first,
		"created_at":       item.CreatedAt,
		"updated_at":       item.UpdatedAt,
	}
}

func revisionJSON(rev store.AdminReviewRevisionSnapshot) map[string]any {
	if rev.ID == "" {
		return nil
	}
	return map[string]any{
		"id":                rev.ID,
		"number":            rev.Number,
		"name":              rev.Name,
		"summary":           rev.Summary,
		"paid_type":         rev.PaidType,
		"state":             rev.State,
		"created_by":        rev.CreatedBy,
		"created_via":       rev.CreatedVia,
		"base_revision_id":  rev.BaseRevisionID,
		"publication_plan": rev.PublicationPlan,
		"attributes":        rev.Attributes,
		"links":             rev.Links,
		"governance":        rev.Governance,
		"media":             mediaJSON(rev.Media),
		"artifacts":         artifactJSON(rev.Artifacts),
		"created_at":        rev.CreatedAt.Format(time.RFC3339),
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

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") || r.URL.Query().Get("format") == "json"
}

var defaultReviewChecklist = []string{"图片合规", "安装包可安装", "描述与实际功能一致", "设备适配正确", "发布计划完整"}

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
