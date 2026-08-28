package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

func writeList(w http.ResponseWriter, items any, total, page, perPage int, err error, code string) {
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody(code, err.Error()))
		return
	}
	totalPages := 0
	if perPage > 0 && total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "page": page, "per_page": perPage, "total_pages": totalPages})
}

func (a *App) handleAdminAPIUsers(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminUsers(r.Context(), store.AdminUserQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("users_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "username": item.Username, "avatar_url": item.AvatarURL,
			"bandbbs_user_id": item.BandBBSUserID, "role": item.Role,
			"resource_count": item.ResourceCount, "ticket_count": item.TicketCount,
			"banned": item.BannedAt != nil, "banned_at": item.BannedAt,
			"frozen": item.CreatorFrozenAt != nil, "creator_frozen_at": item.CreatorFrozenAt,
			"ban_reason": item.BanReason, "created_at": item.CreatedAt,
			"updated_at": item.UpdatedAt, "last_seen_at": item.LastSeenAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "users_failed")
}

func (a *App) handleAdminAPICollections(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminCollections(r.Context(), store.AdminCollectionQuery{
		Search: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("collections_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "name": item.LatestRevisionName, "owner": item.Owner, "owner_id": item.OwnerID,
			"slug": item.Slug, "kind": item.Kind, "platform": item.Platform,
			"state": item.LatestRevisionState, "member_count": item.MemberCount,
			"enabled": item.Enabled, "updated_at": item.UpdatedAt, "created_at": item.CreatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "collections_failed")
}

func (a *App) handleAdminAPIPublications(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminPublications(r.Context(), store.AdminPublicationQuery{
		Search: r.URL.Query().Get("q"), Target: r.URL.Query().Get("target"), State: r.URL.Query().Get("state"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("publications_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "name": item.RevisionName, "target": item.Target, "state": item.State, "error": item.ErrorMessage,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "publications_failed")
}

func (a *App) handleAdminAPIDevices(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("devices_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "name": item.DisplayName, "display_name": item.DisplayName, "codename": item.Codename,
			"platform": item.Platform, "vendor": item.Vendor, "astrobox_id": item.AstroBoxID,
			"enabled": item.Enabled, "resource_count": item.ResourceCount, "artifact_count": item.ArtifactCount,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "devices_failed")
}

func (a *App) handleAdminAPIPlugins(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminPluginsV2(r.Context(), store.AdminPluginQuery{
		Search: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("plugins_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "name": item.Name, "state": item.State, "version": item.Version,
			"author": item.Author, "uploader_id": item.UploaderID, "uploader": item.UploaderName,
			"runtime": item.Runtime, "package_size": item.PackageSize, "icon_sha256": item.IconSHA256,
			"pending_version_id": item.PendingVersionID, "description": item.Description,
			"updated_at": item.UpdatedAt, "created_at": item.CreatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "plugins_failed")
}

func (a *App) handleAdminAPIPluginReview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	decision := strings.TrimSpace(body.Decision)
	note := strings.TrimSpace(body.Note)
	var state string
	switch decision {
	case "approve":
		state = "listed"
	case "reject":
		if note == "" {
			writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "退回插件必须填写理由"))
			return
		}
		state = "rejected"
	default:
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "decision must be approve or reject"))
		return
	}
	detail, err := a.store.AdminPluginV2(r.Context(), r.PathValue("plugin"))
	if errors.Is(err, store.ErrPluginNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "plugin was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("plugin_failed", err.Error()))
		return
	}
	if detail.Plugin.PendingVersionID == "" {
		writeJSON(w, http.StatusConflict, errorBody("plugin_failed", "plugin is not pending review"))
		return
	}
	actor := currentAdmin(r)
	if _, err := a.store.SetPluginState(r.Context(), r.PathValue("plugin"), state, note); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusConflict, errorBody("plugin_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "success", a.clientIP(r), r.UserAgent(), "plugin="+r.PathValue("plugin")+" decision="+decision)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPITickets(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminFeedback(r.Context(), store.AdminFeedbackQuery{
		Search: r.URL.Query().Get("q"), Kind: r.URL.Query().Get("kind"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("tickets_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "kind": item.Kind, "subject": item.Subject, "status": item.Status,
			"username": item.Username, "user_id": item.UserID, "message": item.Message,
			"target_source": item.TargetSource, "target_id": item.TargetID, "target_url": item.TargetURL,
			"resolution": item.Resolution, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt, "closed_at": item.ClosedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "tickets_failed")
}

func (a *App) handleAdminAPIMessages(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminMessages(r.Context(), store.AdminMessageQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("messages_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "user_id": item.UserID, "username": item.Username, "kind": item.Kind,
			"title": item.Title, "body": item.Body, "ref": item.Ref, "read_at": item.ReadAt,
			"created_at": item.CreatedAt, "expires_at": item.ExpiresAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "messages_failed")
}

func (a *App) handleAdminAPIAnnouncements(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminAnnouncementsPage(r.Context(), store.AdminAnnouncementQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("announcements_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "title": item.Title, "body": item.Body, "published_at": item.PublishedAt,
			"created_by": item.CreatedBy, "creator": item.Creator,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "announcements_failed")
}

func (a *App) handleAdminAPIReleases(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminReleases(r.Context(), store.AdminReleaseQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("releases_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "version": item.Version, "minimum_version": item.MinimumVersion,
			"channel": item.Channel, "platform": item.Platform, "arch": item.Arch,
			"download_url": item.DownloadURL, "published_at": item.PublishedAt,
			"enabled": item.Enabled, "revoked_at": item.RevokedAt, "state": item.State(),
			"updated_at": item.UpdatedAt, "created_by": item.CreatedBy, "creator": item.Creator,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "releases_failed")
}

func (a *App) handleAdminAPIBlog(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminBlogPosts(r.Context(), store.AdminBlogQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("blog_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"slug": item.Slug, "type": item.Type, "title": item.Title, "subtitle": item.Subtitle,
			"author": item.Author, "published": item.Published, "published_at": item.PublishedAt,
			"created_at": item.CreatedAt, "updated_at": item.UpdatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "blog_failed")
}

func (a *App) handleAdminAPIAudit(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminAuditLogs(r.Context(), store.AdminAuditLogQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("audit_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "created_at": item.CreatedAt, "actor_user_id": item.ActorUserID,
			"username": item.Username, "action": item.Action, "result": item.Result,
			"ip": item.IP, "user_agent": item.UserAgent, "message": item.Message,
			"target": item.Target, "before": item.Before, "after": item.After, "metadata": item.Metadata,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "audit_failed")
}

func (a *App) handleAdminAPICoinLedger(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminCoinLedgerPage(r.Context(), store.AdminCoinQuery{
		Search: r.URL.Query().Get("q"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("coins_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, map[string]any{
			"id": item.ID, "user_id": item.UserID, "kind": item.Kind, "delta_units": item.DeltaUnits,
			"reference_type": item.ReferenceType, "reference_id": item.ReferenceID, "note": item.Note,
			"username": item.Username, "created_at": item.CreatedAt,
		})
	}
	writeList(w, items, page.Total, page.Page, page.PerPage, nil, "coins_failed")
}

func (a *App) handleAdminAPIHealth(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	dbStatus := "ok"
	if err := a.store.Ping(r.Context()); err != nil {
		dbStatus = err.Error()
	}
	diagnostics, err := a.store.AdminHealthDiagnostics(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("health_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{{"db": dbStatus, "latency": time.Since(started).Round(time.Millisecond).String(), "version": a.cfg.Version}},
		"total": 1, "page": 1, "per_page": 1, "diagnostics": diagnostics,
	})
}

func (a *App) handleAdminAPISettings(w http.ResponseWriter, r *http.Request) {
	var attributes any
	if a.creator != nil {
		attributes, _ = a.creator.Attributes(r.Context(), true)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []map[string]any{{
			"bandbbs_client_id": a.cfg.BandBBS.ClientID,
			"github_client_id":  a.cfg.GitHub.ClientID,
			"public_url":        a.cfg.PublicURL,
		}},
		"total": 1, "page": 1, "per_page": 1, "attributes": attributes,
	})
}

func isAdminDraft(rev store.AdminRevision) bool {
	return rev.State == "draft" && rev.CreatedVia == "admin"
}

func isPendingReview(rev store.AdminRevision) bool {
	return rev.State == "submitted" && rev.ReviewState == "pending"
}

func pickWorkingRevision(revisions []store.AdminRevision, currentID string) store.AdminRevision {
	for _, rev := range revisions {
		if isAdminDraft(rev) {
			return rev
		}
	}
	for _, rev := range revisions {
		if isPendingReview(rev) {
			return rev
		}
	}
	if currentID != "" {
		for _, rev := range revisions {
			if rev.ID == currentID {
				return rev
			}
		}
	}
	if len(revisions) == 0 {
		return store.AdminRevision{}
	}
	return revisions[0]
}

func (a *App) handleAdminAPIResource(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminResource(r.Context(), r.PathValue("resource"))
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "resource was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("resource_failed", err.Error()))
		return
	}
	revision := pickWorkingRevision(detail.Revisions, detail.Resource.CurrentRevisionID)
	revDetail, err := a.store.AdminResourceRevision(r.Context(), detail.Resource.ID, revision.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("resource_failed", err.Error()))
		return
	}
	governance, _ := a.store.AdminRevisionGovernance(r.Context(), revision.ID)
	attributes, _ := a.creator.Attributes(r.Context(), true)
	devices, _ := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Page: 1, PerPage: 200})
	collections, _ := a.store.AdminCollections(r.Context(), store.AdminCollectionQuery{Page: 1, PerPage: 100})
	revisions := make([]map[string]any, 0, len(detail.Revisions))
	for _, item := range detail.Revisions {
		revisions = append(revisions, map[string]any{
			"id": item.ID, "number": item.Number, "name": item.Name, "state": item.State, "created_via": item.CreatedVia,
			"review_state": item.ReviewState, "created_at": item.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": detail.Resource.ID, "name": revision.Name, "summary": revision.Summary, "paid_type": revision.PaidType,
		"slug": detail.Resource.Slug, "kind": detail.Resource.Kind, "platform": detail.Resource.Platform,
		"owner": detail.Resource.Owner, "owner_id": detail.Resource.OwnerID,
		"moderation": detail.Resource.ModerationState, "moderation_reason": detail.Resource.ModerationReason,
		"revision_id": revision.ID, "base_revision_id": revision.BaseRevisionID, "revision_state": revision.State,
		"can_submit": isAdminDraft(revision), "pending": isPendingReview(revision),
		"editable": isAdminDraft(revision) || isPendingReview(revision), "attributes": revision.Attributes,
		"links": revDetail.Links, "publication_plan": json.RawMessage(revision.PublicationPlan),
		"governance": governance, "media": mediaJSON(revDetail.Media), "artifacts": artifactJSON(revDetail.Artifacts),
		"revisions": revisions, "publications": detail.Publications, "bindings": detail.Bindings, "events": detail.Events,
		"attribute_catalog": attributes, "devices": devices.Items, "collections": collections.Items,
	})
}

func (a *App) handleAdminAPIResourceDraft(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name            string            `json:"name"`
		Summary         string            `json:"summary"`
		PaidType        string            `json:"paid_type"`
		RevisionID      string            `json:"revision_id"`
		Attributes      []string          `json:"attributes"`
		Links           []store.AdminLink `json:"links"`
		PublicationPlan json.RawMessage   `json:"publication_plan"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	resourceID := r.PathValue("resource")
	full, err := a.store.AdminResource(r.Context(), resourceID)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("draft_failed", err.Error()))
		return
	}
	working := pickWorkingRevision(full.Revisions, full.Resource.CurrentRevisionID)
	sourceID := working.ID
	if sourceID == "" {
		sourceID = strings.TrimSpace(body.RevisionID)
	}
	detail, err := a.store.AdminResourceRevision(r.Context(), resourceID, sourceID)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("draft_failed", err.Error()))
		return
	}
	draftID, baseID := sourceID, detail.Revision.BaseRevisionID
	editable := isAdminDraft(detail.Revision) || isPendingReview(detail.Revision)
	if !editable {
		draftID = ""
		baseID = detail.Revision.ID
	} else if baseID == "" {
		baseID = detail.Revision.ID
	}
	links := body.Links
	if body.Links == nil {
		links = detail.Links
	}
	plan := body.PublicationPlan
	if len(plan) == 0 {
		plan = append(json.RawMessage(nil), detail.Revision.PublicationPlan...)
	}
	actor := currentAdmin(r)
	revisionID, err := a.store.AdminSaveRevisionDraft(r.Context(), resourceID, store.AdminRevisionDraftInput{
		DraftRevisionID: draftID,
		BaseRevisionID:  baseID,
		Name:            body.Name,
		Summary:         body.Summary,
		PaidType:        body.PaidType,
		Attributes:      body.Attributes,
		Links:           links,
		PublicationPlan: plan,
	}, actor)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("draft_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revision_id": revisionID})
}

func (a *App) handleAdminAPIResourceSubmit(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	if err := a.store.AdminSubmitRevisionDraft(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("submit_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPICommentDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if strings.TrimSpace(body.Action) == "hide" && strings.TrimSpace(body.Note) == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "隐藏评论必须填写理由"))
		return
	}
	actor := currentAdmin(r)
	if err := a.store.AdminModerateComment(r.Context(), r.PathValue("comment"), body.Action, actor.UserID, body.Note); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("comment_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIUserState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
		Role   string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	actor := currentAdmin(r)
	if _, err := a.store.AdminManageUser(r.Context(), r.PathValue("user"), body.Action, body.Reason, body.Role, actor); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("user_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIPublicationAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	actor := currentAdmin(r)
	if _, err := a.store.AdminManagePublication(r.Context(), r.PathValue("publication"), body.Action); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "publication."+body.Action, "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusConflict, errorBody("publication_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "publication."+body.Action, "success", a.clientIP(r), r.UserAgent(), "publication="+r.PathValue("publication"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPITicketAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Status string `json:"status"`
		Reply  string `json:"reply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if strings.TrimSpace(body.Status) == "replied" && strings.TrimSpace(body.Reply) == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "回复不能为空"))
		return
	}
	actor := currentAdmin(r)
	if _, err := a.store.UpdateFeedback(r.Context(), r.PathValue("ticket"), store.FeedbackUpdate{
		Status: body.Status, Reply: body.Reply, AuthorID: actor.UserID,
	}); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("ticket_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
