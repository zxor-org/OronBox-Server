package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *Handler) HandleAdminAPIReviewChecklist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []string `json:"items"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	actor := a.actor(r)
	if err := a.store.AdminSaveReviewChecklist(r.Context(), r.PathValue("review"), actor.UserID, body.Items); err != nil {
		a.audit(r, "review.checklist", "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("review_failed", err.Error()))
		return
	}
	a.audit(r, "review.checklist", "success", "review="+r.PathValue("review"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIReviewBulk(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action     string   `json:"action"`
		IDs        []string `json:"ids"`
		ReviewerID string   `json:"reviewer_id"`
		Priority   int      `json:"priority"`
		Note       string   `json:"note"`
		Grade      string   `json:"grade"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	actor := a.actor(r)
	var err error
	switch body.Action {
	case "assign":
		err = a.store.AdminAssignReviews(r.Context(), body.IDs, body.ReviewerID, actor.UserID)
	case "priority":
		err = a.store.AdminSetReviewPriority(r.Context(), body.IDs, body.Priority, actor.UserID)
	case "approve", "reject":
		if body.Action == "reject" && strings.TrimSpace(body.Note) == "" {
			httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "批量退回必须填写理由"))
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
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "unknown bulk action"))
		return
	}
	if err != nil {
		a.audit(r, "review.bulk."+body.Action, "failure", err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("review_failed", err.Error()))
		return
	}
	a.audit(r, "review.bulk."+body.Action, "success", strings.Join(body.IDs, ","))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *Handler) HandleAdminAPIReviews(w http.ResponseWriter, r *http.Request) {
	from, to := TimeRange(r.URL.Query())
	page, err := a.store.AdminReviews(r.Context(), store.AdminReviewQuery{
		Search:  r.URL.Query().Get("q"),
		Kind:    r.URL.Query().Get("kind"),
		Target:  r.URL.Query().Get("target"),
		Owner:   r.URL.Query().Get("owner"),
		State:   r.URL.Query().Get("state"),
		From:    from,
		To:      to,
		Sort:    r.URL.Query().Get("sort"),
		Page:    httpx.PositiveInt(r.URL.Query().Get("page"), 1),
		PerPage: httpx.PositiveInt(r.URL.Query().Get("per_page"), 40),
	})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("reviews_failed", err.Error()))
		return
	}
	items := make([]map[string]any, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, reviewItemJSON(item))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": page.Total, "page": page.Page, "per_page": page.PerPage,
	})
}

func (a *Handler) HandleAdminAPIReview(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminReview(r.Context(), r.PathValue("review"))
	if errors.Is(err, store.ErrAdminReviewNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "review case was not found"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("review_failed", err.Error()))
		return
	}
	events, _ := a.store.AdminReviewEvents(r.Context(), detail.Review.ID)
	reviewers, _ := a.store.AdminReviewers(r.Context())
	attributes, _ := a.creator.Attributes(r.Context(), false)
	devices, _ := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Page: 1, PerPage: 200})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
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

func (a *Handler) HandleAdminAPIReviewDecision(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision   string   `json:"decision"`
		Note       string   `json:"note"`
		Grade      string   `json:"grade"`
		Items      []string `json:"items"`
		Attributes []string `json:"attributes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "invalid json"))
		return
	}
	if body.Decision != "approve" && body.Decision != "reject" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "decision must be approve or reject"))
		return
	}
	detail, err := a.store.AdminReview(r.Context(), r.PathValue("review"))
	if errors.Is(err, store.ErrAdminReviewNotFound) {
		httpx.WriteJSON(w, http.StatusNotFound, httpx.ErrorBody("not_found", "review case was not found"))
		return
	}
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("review_failed", err.Error()))
		return
	}
	note := strings.TrimSpace(body.Note)
	approved := body.Decision == "approve"
	if !approved && note == "" {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", "退回必须填写理由"))
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
	actor := a.actor(r)
	if err := a.creator.Review(r.Context(), detail.Current.ID, actor.UserID, approved, note, items, attributes, grade); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("review_failed", err.Error()))
		return
	}
	result := "rejected"
	if approved {
		result = "approved"
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.review", result, a.clientIP(r), r.UserAgent(), "revision="+detail.Current.ID)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

var defaultReviewChecklist = []string{"图片合规", "安装包可安装", "描述与实际功能一致", "设备适配正确", "发布计划完整"}

func (a *Handler) HandleAdminCollectionReviewQueue(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.CollectionReviewQueue(r.Context())
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("collection_review_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *Handler) ApplyCollectionReview(ctx context.Context, revisionID string, actor store.AdminSession, approve bool, note string) error {
	note = strings.TrimSpace(note)
	if !approve && note == "" {
		return errCollectionRejectReasonRequired
	}
	return a.creator.ReviewCollection(ctx, revisionID, actor.UserID, approve, note)
}

func (a *Handler) HandleAdminCollectionReviewDecision(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := httpx.DecodeJSON(r, &request); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	actor := a.actor(r)
	err := a.ApplyCollectionReview(r.Context(), r.PathValue("revision"), actor, request.Approve, request.Note)
	if errors.Is(err, errCollectionRejectReasonRequired) {
		httpx.WriteJSON(w, http.StatusBadRequest, httpx.ErrorBody("invalid_request", err.Error()))
		return
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		httpx.WriteJSON(w, http.StatusConflict, httpx.ErrorBody("collection_review_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "success", a.clientIP(r), r.UserAgent(), "revision="+r.PathValue("revision"))
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}
