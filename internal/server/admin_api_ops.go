package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func readJSON(r *http.Request, dest any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(dest)
}

func (a *App) audit(r *http.Request, action, result, detail string) {
	_ = a.store.RecordAudit(r.Context(), currentAdmin(r), action, result, a.clientIP(r), r.UserAgent(), detail)
}

func (a *App) handleAdminAPIAnalytics(w http.ResponseWriter, r *http.Request) {
	rangeName := r.URL.Query().Get("range")
	if rangeName != "90d" && rangeName != "12m" {
		rangeName = "30d"
	}
	data, err := a.store.AdminAnalytics(r.Context(), rangeName)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("analytics_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (a *App) handleAdminAPIOverview(w http.ResponseWriter, r *http.Request) {
	reviews, err := a.store.AdminReviews(r.Context(), store.AdminReviewQuery{State: "pending", Sort: "sla", Page: 1, PerPage: 40})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("overview_failed", err.Error()))
		return
	}
	overdue := 0
	for _, item := range reviews.Items {
		if item.Overdue() {
			overdue++
		}
	}
	comments, _ := a.store.AdminComments(r.Context(), store.AdminCommentQuery{State: "review", Page: 1, PerPage: 1})
	tickets, _ := a.store.AdminFeedback(r.Context(), store.AdminFeedbackQuery{Kind: store.FeedbackKindReports, Page: 1, PerPage: 1})
	publications, _ := a.store.AdminPublications(r.Context(), store.AdminPublicationQuery{State: "failed", Page: 1, PerPage: 1})
	plugins, _ := a.store.AdminPluginsV2(r.Context(), store.AdminPluginQuery{State: "pending", Page: 1, PerPage: 1})
	writeJSON(w, http.StatusOK, map[string]any{
		"pending_reviews": reviews.Total, "overdue_reviews": overdue, "pending_comments": comments.Total,
		"open_reports": tickets.Total, "failed_publications": publications.Total, "pending_plugins": plugins.Total,
	})
}

func (a *App) handleAdminAPICatalog(w http.ResponseWriter, r *http.Request) {
	attributes, _ := a.creator.Attributes(r.Context(), true)
	devices, _ := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Page: 1, PerPage: 200})
	reviewers, _ := a.store.AdminReviewers(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"attributes": attributes, "devices": devices.Items, "reviewers": reviewers, "checklist": defaultReviewChecklist})
}

func (a *App) handleAdminAPIReviewChecklist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []string `json:"items"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	actor := currentAdmin(r)
	if err := a.store.AdminSaveReviewChecklist(r.Context(), r.PathValue("review"), actor.UserID, body.Items); err != nil {
		a.audit(r, "review.checklist", "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("review_failed", err.Error()))
		return
	}
	a.audit(r, "review.checklist", "success", "review="+r.PathValue("review"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIReviewBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action     string   `json:"action"`
		IDs        []string `json:"ids"`
		ReviewerID string   `json:"reviewer_id"`
		Priority   int      `json:"priority"`
		Note       string   `json:"note"`
		Grade      string   `json:"grade"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	actor := currentAdmin(r)
	var err error
	switch body.Action {
	case "assign":
		err = a.store.AdminAssignReviews(r.Context(), body.IDs, body.ReviewerID, actor.UserID)
	case "priority":
		err = a.store.AdminSetReviewPriority(r.Context(), body.IDs, body.Priority, actor.UserID)
	case "approve", "reject":
		if body.Action == "reject" && strings.TrimSpace(body.Note) == "" {
			writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "批量退回必须填写理由"))
			return
		}
		grade := body.Grade
		if grade != "featured" {
			grade = "standard"
		}
		for _, id := range body.IDs {
			detail, lookupErr := a.store.AdminReview(r.Context(), id)
			if lookupErr != nil {
				err = lookupErr
				break
			}
			if reviewErr := a.creator.Review(r.Context(), detail.Current.ID, actor.UserID, body.Action == "approve", body.Note, detail.Review.Items, detail.Current.Attributes, grade); reviewErr != nil {
				err = reviewErr
				break
			}
		}
	default:
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "unknown bulk action"))
		return
	}
	if err != nil {
		a.audit(r, "review.bulk."+body.Action, "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("review_failed", err.Error()))
		return
	}
	a.audit(r, "review.bulk."+body.Action, "success", strings.Join(body.IDs, ","))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIResourceState(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	actor := currentAdmin(r)
	result, err := a.store.AdminManageResource(r.Context(), r.PathValue("resource"), body.Action, body.Reason, actor)
	if err != nil {
		a.audit(r, "resource."+body.Action, "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("resource_failed", err.Error()))
		return
	}
	a.audit(r, "resource."+body.Action, "success", "resource="+result.ID+" moderation="+result.ModerationState)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": result.Deleted})
}

func (a *App) handleAdminAPIResourceDiscard(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	if err := a.store.AdminDiscardRevisionDraft(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor); err != nil {
		a.audit(r, "resource.draft.discard", "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("draft_failed", err.Error()))
		return
	}
	a.audit(r, "resource.draft.discard", "success", "revision="+r.PathValue("revision"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIResourceRollback(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	id, err := a.store.AdminCreateRollbackRevision(r.Context(), r.PathValue("resource"), r.PathValue("revision"), actor)
	if err != nil {
		a.audit(r, "resource.rollback", "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("rollback_failed", err.Error()))
		return
	}
	a.audit(r, "resource.rollback", "success", "revision="+id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revision_id": id})
}

func (a *App) handleAdminAPIResourceGovernance(w http.ResponseWriter, r *http.Request) {
	var input store.AdminRevisionGovernance
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.store.AdminSaveRevisionGovernance(r.Context(), r.PathValue("resource"), r.PathValue("revision"), input); err != nil {
		a.audit(r, "resource.governance", "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("governance_failed", err.Error()))
		return
	}
	a.audit(r, "resource.governance", "success", "revision="+r.PathValue("revision"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIHome(w http.ResponseWriter, r *http.Request) {
	banners, err := a.store.ListHomeBanners(r.Context(), false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("home_failed", err.Error()))
		return
	}
	sections, err := a.store.ListHomeSections(r.Context(), false)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("home_failed", err.Error()))
		return
	}
	out := make([]map[string]any, 0, len(sections))
	for _, section := range sections {
		cards, _ := a.store.ListHomeSectionCards(r.Context(), section.ID)
		out = append(out, map[string]any{"section": section, "cards": cards})
	}
	writeJSON(w, http.StatusOK, map[string]any{"banners": banners, "sections": out})
}

func (a *App) handleAdminAPIHomeBanner(w http.ResponseWriter, r *http.Request) {
	var banner store.HomeBanner
	if err := readJSON(r, &banner); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	id := r.PathValue("banner")
	if id == "" {
		banner.ID = uuid.NewString()
		if err := a.store.CreateHomeBanner(r.Context(), banner); err != nil {
			a.audit(r, "home.banner.create", "failure", err.Error())
			writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
			return
		}
		a.audit(r, "home.banner.create", "success", "banner="+banner.ID)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": banner.ID})
		return
	}
	banner.ID = id
	if err := a.store.UpdateHomeBanner(r.Context(), banner); err != nil {
		a.audit(r, "home.banner.save", "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
		return
	}
	a.audit(r, "home.banner.save", "success", "banner="+id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIHomeBannerDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteHomeBanner(r.Context(), r.PathValue("banner")); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
		return
	}
	a.audit(r, "home.banner.delete", "success", "banner="+r.PathValue("banner"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIHomeMove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Delta int `json:"delta"`
	}
	if err := readJSON(r, &body); err != nil || (body.Delta != 1 && body.Delta != -1) {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "delta must be 1 or -1"))
		return
	}
	kind := r.PathValue("kind")
	id := r.PathValue("id")
	var err error
	switch kind {
	case "banners":
		err = a.store.MoveHomeBanner(r.Context(), id, body.Delta)
	case "sections":
		err = a.store.MoveHomeSection(r.Context(), id, body.Delta)
	case "cards":
		err = a.store.MoveHomeSectionCard(r.Context(), id, r.URL.Query().Get("section_id"), body.Delta)
	default:
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "unknown home item"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIHomeSection(w http.ResponseWriter, r *http.Request) {
	var section store.HomeSection
	if err := readJSON(r, &section); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.store.CreateHomeSection(r.Context(), section); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
		return
	}
	a.audit(r, "home.section.create", "success", "section="+section.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIHomeSectionDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteHomeSection(r.Context(), r.PathValue("section")); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIHomeCard(w http.ResponseWriter, r *http.Request) {
	var card store.HomeSectionCard
	if err := readJSON(r, &card); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	card.ID = uuid.NewString()
	if err := a.store.CreateHomeSectionCard(r.Context(), card); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": card.ID})
}

func (a *App) handleAdminAPIHomeCardDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteHomeSectionCard(r.Context(), r.PathValue("card")); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("home_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPICommentBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
		Note   string   `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if body.Action == "hide" && strings.TrimSpace(body.Note) == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "批量隐藏必须填写理由"))
		return
	}
	actor := currentAdmin(r)
	if err := a.store.AdminModerateCommentsBatch(r.Context(), body.IDs, body.Action, actor.UserID, body.Note); err != nil {
		a.audit(r, "comment.bulk."+body.Action, "failure", err.Error())
		writeJSON(w, http.StatusConflict, errorBody("comment_failed", err.Error()))
		return
	}
	a.audit(r, "comment.bulk."+body.Action, "success", strings.Join(body.IDs, ","))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIModerationPrompt(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt string `json:"prompt"`
		Text   string `json:"text"`
		Test   bool   `json:"test"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if body.Test {
		prompt := strings.TrimSpace(body.Prompt)
		if prompt == "" {
			prompt, _ = a.store.Setting(r.Context(), "moderation.prompt", defaultModerationPrompt)
		}
		if a.moderation == nil || !a.moderation.Enabled() {
			writeJSON(w, http.StatusServiceUnavailable, errorBody("moderation_unavailable", "审核模型未启用"))
			return
		}
		verdict, err := a.moderation.Review(r.Context(), prompt, body.Text)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody("moderation_unavailable", err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "verdict": verdict})
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		prompt = defaultModerationPrompt
	}
	if err := a.store.SetSetting(r.Context(), "moderation.prompt", prompt); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("settings_failed", err.Error()))
		return
	}
	a.audit(r, "moderation.prompt", "success", "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIUserDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminUserDetail(r.Context(), r.PathValue("user"), store.AdminUserDetailQuery{})
	if err != nil {
		if errors.Is(err, store.ErrAdminUserNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("not_found", err.Error()))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorBody("user_detail_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleAdminAPIUserSessions(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		All       bool   `json:"all"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	var err error
	if body.All {
		_, err = a.store.AdminRevokeAllUserSessions(r.Context(), r.PathValue("user"))
	} else {
		err = a.store.AdminRevokeUserSession(r.Context(), r.PathValue("user"), body.SessionID)
	}
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("user_failed", err.Error()))
		return
	}
	a.audit(r, "user.sessions.revoke", "success", "user="+r.PathValue("user"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIMessageSend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserIDs []string `json:"user_ids"`
		Title   string   `json:"title"`
		Body    string   `json:"body"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	count, err := a.store.CreateAdminMessages(r.Context(), body.UserIDs, body.Title, body.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("message_failed", err.Error()))
		return
	}
	a.audit(r, "message.send", "success", "count="+strconv.FormatInt(count, 10))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": count})
}

func (a *App) handleAdminAPIAnnouncementCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.store.CreateAnnouncement(r.Context(), currentAdmin(r).UserID, body.Title, body.Body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("announcement_failed", err.Error()))
		return
	}
	a.audit(r, "announcement.publish", "success", body.Title)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIAnnouncementDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteAnnouncement(r.Context(), r.PathValue("announcement")); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("announcement_failed", err.Error()))
		return
	}
	a.audit(r, "announcement.delete", "success", r.PathValue("announcement"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIBlogGet(w http.ResponseWriter, r *http.Request) {
	post, err := a.store.AdminBlogPost(r.Context(), r.PathValue("slug"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, post)
}

func (a *App) handleAdminAPIBlogSave(w http.ResponseWriter, r *http.Request) {
	var post store.BlogPost
	if err := readJSON(r, &post); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	slug := r.PathValue("slug")
	if slug == "" {
		if err := a.store.UpsertBlogPost(r.Context(), post); err != nil {
			writeJSON(w, http.StatusConflict, errorBody("blog_failed", err.Error()))
			return
		}
		a.audit(r, "blog.create", "success", "slug="+post.Slug)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	post.Slug = slug
	if err := a.store.SaveBlogPost(r.Context(), post); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("blog_failed", err.Error()))
		return
	}
	a.audit(r, "blog.save", "success", "slug="+slug)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIBlogDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.store.DeleteBlogPost(r.Context(), r.PathValue("slug")); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("blog_failed", err.Error()))
		return
	}
	a.audit(r, "blog.delete", "success", r.PathValue("slug"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIReleasePublish(w http.ResponseWriter, r *http.Request) {
	var release store.AppRelease
	if err := readJSON(r, &release); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	created, err := a.store.PublishAppRelease(r.Context(), release, currentAdmin(r).UserID)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("release_failed", err.Error()))
		return
	}
	a.audit(r, "release.publish", "success", "version="+created.Version)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": created.ID})
}

func (a *App) handleAdminAPIReleaseNotes(w http.ResponseWriter, r *http.Request) {
	var input store.AdminReleaseNotesInput
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	item, err := a.store.AdminUpdateReleaseNotes(r.Context(), r.PathValue("release"), input)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("release_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (a *App) handleAdminAPIReleaseStateJSON(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	item, err := a.store.AdminSetReleaseState(r.Context(), r.PathValue("release"), body.Action)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("release_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (a *App) handleAdminAPIDeviceSaveJSON(w http.ResponseWriter, r *http.Request) {
	var input store.AdminDeviceInput
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if id := r.PathValue("device"); id != "" && id != "new" {
		input.ID = id
	}
	item, err := a.store.AdminSaveDevice(r.Context(), input)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("device_failed", err.Error()))
		return
	}
	a.audit(r, "device.save", "success", "device="+item.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "item": item})
}

func (a *App) handleAdminAPIBlobs(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminBlobs(r.Context(), store.AdminBlobQuery{
		Search: r.URL.Query().Get("q"), ReplicaState: r.URL.Query().Get("replica_state"),
		Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("blobs_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "blobs_failed")
}

func (a *App) handleAdminAPIBlob(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminBlob(r.Context(), r.PathValue("sha256"))
	if err != nil {
		if errors.Is(err, store.ErrAdminBlobNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("not_found", "blob was not found"))
		} else {
			writeJSON(w, http.StatusInternalServerError, errorBody("blob_failed", err.Error()))
		}
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleAdminAPIBlobRequeue(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminRequeueBlobReplica(r.Context(), r.PathValue("sha256"))
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("blob_failed", err.Error()))
		return
	}
	a.audit(r, "blob.requeue", "success", r.PathValue("sha256"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "detail": detail})
}

func (a *App) handleAdminAPIOAuthEvents(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthEvents(r.Context(), store.AdminOAuthEventQuery{
		Search: r.URL.Query().Get("q"), Result: r.URL.Query().Get("result"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "oauth_failed")
}

func (a *App) handleAdminAPIOAuthEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid OAuth event id"))
		return
	}
	detail, err := a.store.AdminOAuthEvent(r.Context(), id)
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "OAuth event was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleAdminAPIOAuthStates(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthStates(r.Context(), store.AdminOAuthStateQuery{
		Search: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), From: from, To: to,
		Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "oauth_failed")
}

func (a *App) handleAdminAPIOAuthState(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminOAuthState(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "OAuth state was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleAdminAPIOAuthTickets(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthTickets(r.Context(), store.AdminOAuthTicketQuery{
		Search: r.URL.Query().Get("q"), Status: r.URL.Query().Get("status"), From: from, To: to,
		Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "oauth_failed")
}

func (a *App) handleAdminAPIOAuthTicket(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminOAuthTicket(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "OAuth ticket was not found"))
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("oauth_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleAdminAPIClients(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminClientStats(r.Context(), store.AdminClientStatsQuery{
		Search: r.URL.Query().Get("q"), Result: r.URL.Query().Get("result"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("clients_failed", err.Error()))
		return
	}
	writeList(w, page.Items, page.Total, page.Page, page.PerPage, nil, "clients_failed")
}

func (a *App) handleAdminAPICleanupJSON(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	var body struct {
		Token        string `json:"token"`
		Confirmation string `json:"confirmation"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	preview, err := a.verifyCleanupPreview(strings.TrimSpace(body.Token), actor.UserID, time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("cleanup_failed", err.Error()))
		return
	}
	if body.Confirmation != cleanupConfirmation(preview) {
		writeJSON(w, http.StatusBadRequest, errorBody("cleanup_confirmation_mismatch", "确认短语不匹配"))
		return
	}
	stats, err := a.store.ExecuteExpiredCleanup(r.Context(), preview)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("cleanup_failed", err.Error()))
		return
	}
	a.audit(r, "cleanup.execute", "success", cleanupSummary(stats))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "stats": stats})
}

func (a *App) handleAdminAPIAttribute(w http.ResponseWriter, r *http.Request) {
	var item creator.ResourceAttribute
	if err := readJSON(r, &item); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if err := a.creator.UpsertAttribute(r.Context(), item); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("attribute_failed", err.Error()))
		return
	}
	a.audit(r, "resource_attribute.save", "success", item.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIAttributeDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.creator.DisableAttribute(r.Context(), r.PathValue("attribute")); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("attribute_failed", err.Error()))
		return
	}
	a.audit(r, "resource_attribute.disable", "success", r.PathValue("attribute"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIPluginMetadataJSON(w http.ResponseWriter, r *http.Request) {
	var input store.AdminPluginMetadataRevisionInput
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	input.CreatedBy = currentAdmin(r).UserID
	version, err := a.store.AdminCreatePluginMetadataRevision(r.Context(), r.PathValue("plugin"), input)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("plugin_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

func (a *App) handleAdminAPIPluginStateJSON(w http.ResponseWriter, r *http.Request) {
	var body struct {
		State  string `json:"state"`
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	if _, err := a.store.SetPluginState(r.Context(), r.PathValue("plugin"), body.State, body.Reason); err != nil {
		writeJSON(w, http.StatusConflict, errorBody("plugin_failed", err.Error()))
		return
	}
	a.audit(r, "plugin.state", "success", r.PathValue("plugin")+" "+body.State)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminAPIPublicationsRetry(w http.ResponseWriter, r *http.Request) {
	result, err := a.store.AdminRetryFailedPublications(r.Context(), store.AdminPublicationQuery{
		Search: r.URL.Query().Get("q"), Target: r.URL.Query().Get("target"),
	})
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("publication_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (a *App) handleAdminAPICollectionGet(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminCollection(r.Context(), r.PathValue("collection"))
	if err != nil {
		if errors.Is(err, store.ErrAdminResourceNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("not_found", "collection was not found"))
		} else {
			writeJSON(w, http.StatusInternalServerError, errorBody("collection_failed", err.Error()))
		}
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleAdminAPICollectionSave(w http.ResponseWriter, r *http.Request) {
	var input store.AdminCollectionMetadataInput
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid json"))
		return
	}
	input.CreatedBy = currentAdmin(r).UserID
	revision, err := a.store.AdminUpdateCollectionMetadata(r.Context(), r.PathValue("collection"), input)
	if err != nil {
		writeJSON(w, http.StatusConflict, errorBody("collection_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revision": revision})
}

func (a *App) handleAdminAPITicketDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminFeedbackDetail(r.Context(), r.PathValue("ticket"))
	if err != nil {
		if errors.Is(err, store.ErrFeedbackNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("not_found", "ticket was not found"))
		} else {
			writeJSON(w, http.StatusInternalServerError, errorBody("ticket_failed", err.Error()))
		}
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (a *App) handleAdminAPIAuditDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", "invalid id"))
		return
	}
	item, err := a.store.AdminAuditLog(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrAdminAuditNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("not_found", "audit log was not found"))
		} else {
			writeJSON(w, http.StatusInternalServerError, errorBody("audit_failed", err.Error()))
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *App) handleAdminAPIPluginGet(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminPluginV2(r.Context(), r.PathValue("plugin"))
	if err != nil {
		if errors.Is(err, store.ErrPluginNotFound) {
			writeJSON(w, http.StatusNotFound, errorBody("not_found", "plugin was not found"))
		} else {
			writeJSON(w, http.StatusInternalServerError, errorBody("plugin_failed", err.Error()))
		}
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
