package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
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
	a.render(w, "admin_review_detail", map[string]any{"Title": detail.Current.Name, "Detail": detail, "Attributes": attributes, "Saved": r.URL.Query().Get("saved") != ""})
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
	items := reviewItems(r.FormValue("items"))
	err := a.store.AdminSaveReviewChecklist(r.Context(), r.PathValue("review"), items)
	actor := currentAdmin(r)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "review.checklist.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "review.checklist.save", "success", a.clientIP(r), r.UserAgent(), "review="+r.PathValue("review"))
	http.Redirect(w, r, "/admin/review/"+r.PathValue("review")+"?saved=1", http.StatusFound)
}

func (a *App) handleAdminReviewBulk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor, action := currentAdmin(r), r.FormValue("bulk_action")
	var err error
	switch action {
	case "assign":
		err = a.store.AdminAssignReviews(r.Context(), r.Form["review_ids"], r.FormValue("reviewer_id"))
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
