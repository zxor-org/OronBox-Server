package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminReviewDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminReview(r.Context(), r.PathValue("review"))
	if errors.Is(err, store.ErrAdminReviewNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	attributes, err := a.creator.Attributes(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// The device picker has to follow the resource being reviewed. Hardcoding
	// one platform silently offers the wrong hardware once a second one ships.
	devices, err := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{Platform: detail.Review.ResourcePlatform, State: "enabled", Page: 1, PerPage: 100})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	collections, err := a.store.AdminCollections(r.Context(), store.AdminCollectionQuery{Owner: detail.Review.OwnerID, Kind: detail.Review.ResourceKind, Page: 1, PerPage: 100})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	events, err := a.store.AdminReviewEvents(r.Context(), detail.Review.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.render(w, r, "admin_review_detail", map[string]any{
		"Title": detail.Current.Name, "Detail": detail, "Attributes": attributes,
		"Devices": devices.Items, "Collections": collections.Items, "Events": events,
		"Saved": r.URL.Query().Get("saved") != "", "Action": r.URL.Query().Get("action"),
	})
}

func adminReviewReturn(r *http.Request) string {
	value := strings.TrimSpace(r.FormValue("return_to"))
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != "/admin/review" {
		return "/admin/review"
	}
	return parsed.RequestURI()
}

func (a *App) handleAdminReviewChecklist(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	items := reviewChecklistFromForm(r)
	actor := currentAdmin(r)
	err := a.store.AdminSaveReviewChecklist(r.Context(), r.PathValue("review"), actor.UserID, items)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "review.checklist.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "review.checklist.save", "success", a.clientIP(r), r.UserAgent(), "review="+r.PathValue("review"))
	if r.Header.Get("HX-Request") == "true" {
		http.Redirect(w, r, "/admin/review/"+r.PathValue("review")+"?saved=1", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/review/"+r.PathValue("review")+"?saved=1", http.StatusFound)
}

func (a *App) handleAdminReviewBulk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor, action := currentAdmin(r), r.FormValue("bulk_action")
	note := strings.TrimSpace(r.FormValue("note"))
	var err error
	switch action {
	case "assign":
		err = a.store.AdminAssignReviews(r.Context(), r.Form["review_ids"], r.FormValue("reviewer_id"), actor.UserID)
	case "approve":
		err = a.creator.ReviewBatch(r.Context(), r.Form["review_ids"], actor.UserID, true, note)
	case "reject":
		if note == "" {
			http.Error(w, "批量退回必须填写理由，创作者需要知道要改什么", http.StatusBadRequest)
			return
		}
		err = a.creator.ReviewBatch(r.Context(), r.Form["review_ids"], actor.UserID, false, note)
	case "priority":
		err = a.store.AdminSetReviewPriority(r.Context(), r.Form["review_ids"], positiveInt(r.FormValue("priority"), 0), actor.UserID)
	default:
		http.Error(w, "无效的批量操作", http.StatusBadRequest)
		return
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "review.bulk."+action, "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, "批量操作已拒绝，未修改任何审核单："+err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "review.bulk."+action, "success", a.clientIP(r), r.UserAgent(), strings.Join(r.Form["review_ids"], ","))
	returnTo := adminReviewReturn(r)
	separator := "?"
	if strings.Contains(returnTo, "?") {
		separator = "&"
	}
	http.Redirect(w, r, returnTo+separator+"bulk=1", http.StatusFound)
}

func reviewItems(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	return mergeReviewItems(nil, lines)
}

func mergeReviewItems(checked []string, extras []string) []string {
	seen := map[string]struct{}{}
	items := make([]string, 0, len(checked)+len(extras))
	for _, item := range append(append([]string{}, checked...), extras...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		items = append(items, item)
	}
	return items
}

func reviewChecklistFromForm(r *http.Request) []string {
	return mergeReviewItems(r.Form["item"], strings.Split(strings.ReplaceAll(r.FormValue("items"), "\r\n", "\n"), "\n"))
}

func (a *App) handleAdminReview(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminReviews(r.Context(), store.AdminReviewQuery{Search: r.URL.Query().Get("q"), Kind: r.URL.Query().Get("kind"), Target: r.URL.Query().Get("target"), Owner: r.URL.Query().Get("owner"), State: r.URL.Query().Get("state"), From: from, To: to, Sort: r.URL.Query().Get("sort"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reviewers, err := a.store.AdminReviewers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_review", map[string]any{
		"Title": "审核中心", "Items": page.Items, "Page": page, "Query": page.Query, "Pager": web.NewPagination("/admin/review", r.URL.Query(), page.Page, page.PerPage, page.Total), "Decided": r.URL.Query().Get("decided") != "", "BulkDone": r.URL.Query().Get("bulk") != "", "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Reviewers": reviewers, "ReturnTo": r.URL.RequestURI(),
	})
}

func (a *App) handleAdminReviewDecision(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/review", http.StatusFound)
		return
	}
	decision := r.FormValue("decision")
	if decision != "approve" && decision != "reject" {
		http.Redirect(w, r, "/admin/review", http.StatusFound)
		return
	}
	revisionID := r.PathValue("revision")
	note := strings.TrimSpace(r.FormValue("note"))
	grade := strings.TrimSpace(r.FormValue("curation_grade"))
	if grade == "" {
		grade = "standard"
	}
	approved := decision == "approve"
	actor := currentAdmin(r)
	if !approved && note == "" {
		http.Error(w, "退回必须填写理由，创作者需要知道要改什么", http.StatusBadRequest)
		return
	}
	if err := a.creator.Review(r.Context(), revisionID, actor.UserID, approved, note, reviewChecklistFromForm(r), r.Form["attributes"], grade); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	result := "rejected"
	if approved {
		result = "approved"
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.review", result, a.clientIP(r), r.UserAgent(), "revision="+revisionID)
	http.Redirect(w, r, "/admin/review?decided=1", http.StatusFound)
}
