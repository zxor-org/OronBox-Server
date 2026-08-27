package server

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminFeedback(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminFeedback(r.Context(), store.AdminFeedbackQuery{
		Kind:         r.URL.Query().Get("kind"),
		Status:       r.URL.Query().Get("status"),
		Search:       r.URL.Query().Get("q"),
		TargetSource: r.URL.Query().Get("source"),
		Page:         positiveInt(r.URL.Query().Get("page"), 1),
		PerPage:      positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_feedback", map[string]any{
		"Title":    "反馈",
		"Page":     page,
		"Pager":    web.NewPagination("/admin/feedback", r.URL.Query(), page.Page, page.PerPage, page.Total),
		"Items":    page.Items,
		"Query":    page.Query,
		"Replied":  r.URL.Query().Get("replied") != "",
		"ReturnTo": r.URL.RequestURI(),
	})
}

func (a *App) handleAdminFeedbackReply(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/feedback", http.StatusFound)
		return
	}
	ticketID := r.PathValue("ticket")
	ticket, err := a.store.Feedback(r.Context(), ticketID, "", true)
	if errors.Is(err, store.ErrFeedbackNotFound) || (err == nil && ticket.Kind != "feedback") {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	actor := currentAdmin(r)
	a.handleAdminFeedbackMutation(w, r, ticket, actor, "/admin/feedback/"+ticketID)
}

func (a *App) handleAdminFeedbackDetail(w http.ResponseWriter, r *http.Request) {
	a.renderAdminFeedbackDetail(w, r, false)
}

func (a *App) handleAdminReports(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminFeedback(r.Context(), store.AdminFeedbackQuery{
		Kind:         store.FeedbackKindReports,
		Status:       r.URL.Query().Get("status"),
		Search:       r.URL.Query().Get("q"),
		TargetSource: r.URL.Query().Get("source"),
		Page:         positiveInt(r.URL.Query().Get("page"), 1),
		PerPage:      positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_reports", map[string]any{
		"Title":    "举报",
		"Page":     page,
		"Pager":    web.NewPagination("/admin/reports", r.URL.Query(), page.Page, page.PerPage, page.Total),
		"Items":    page.Items,
		"Query":    page.Query,
		"Action":   r.URL.Query().Get("action"),
		"ReturnTo": r.URL.RequestURI(),
	})
}

func (a *App) handleAdminReport(w http.ResponseWriter, r *http.Request) {
	a.renderAdminFeedbackDetail(w, r, true)
}

func (a *App) renderAdminFeedbackDetail(w http.ResponseWriter, r *http.Request, reportsOnly bool) {
	detail, err := a.store.AdminFeedbackDetail(r.Context(), r.PathValue("ticket"))
	if errors.Is(err, store.ErrFeedbackNotFound) || (err == nil && reportsOnly && !store.IsReportKind(detail.Ticket.Kind)) || (err == nil && !reportsOnly && detail.Ticket.Kind != store.FeedbackKindFeedback) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_report_detail", map[string]any{
		"Title":    detail.Ticket.Subject,
		"Item":     detail.Ticket,
		"Ticket":   detail.Ticket,
		"Detail":   detail,
		"IsReport": store.IsReportKind(detail.Ticket.Kind),
		"BackURL":  adminFeedbackReturnURL(r.URL.Query().Get("return_to"), reportsOnly),
		"ReturnTo": r.URL.Query().Get("return_to"),
		"Action":   r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminReportUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	ticketID := r.PathValue("ticket")
	ticket, err := a.store.Feedback(r.Context(), ticketID, "", true)
	if errors.Is(err, store.ErrFeedbackNotFound) || (err == nil && !store.IsReportKind(ticket.Kind)) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	actor := currentAdmin(r)
	a.handleAdminFeedbackMutation(w, r, ticket, actor, "/admin/reports/"+ticketID)
}

func (a *App) handleAdminFeedbackMutation(w http.ResponseWriter, r *http.Request, ticket store.FeedbackTicket, actor store.AdminSession, detailPath string) {
	action := strings.TrimSpace(r.FormValue("action"))
	if action == "" {
		action = "public_reply"
	}
	var err error
	auditAction := "feedback." + action
	switch action {
	case "internal_note":
		_, err = a.store.AddFeedbackInternalNote(r.Context(), ticket.ID, actor.UserID, r.FormValue("internal_note"))
	case "public_reply", "status":
		status := strings.TrimSpace(r.FormValue("status"))
		message := strings.TrimSpace(r.FormValue("message"))
		if status == "" {
			status = ticket.Status
			if action == "public_reply" && (status == "open" || status == "investigating") {
				status = "replied"
			}
		}
		if action == "public_reply" && message == "" {
			err = errors.New("reply is required")
		} else if action == "status" && status == ticket.Status {
			err = errors.New("select a different status")
		} else {
			_, err = a.store.UpdateFeedback(r.Context(), ticket.ID, store.FeedbackUpdate{Status: status, Reply: message, AuthorID: actor.UserID})
		}
	default:
		err = errors.New("invalid feedback action")
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, auditAction, "failure", a.clientIP(r), r.UserAgent(), "ticket="+ticket.ID+" ticket_kind="+ticket.Kind+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, auditAction, "success", a.clientIP(r), r.UserAgent(), "ticket="+ticket.ID+" ticket_kind="+ticket.Kind+" status="+strings.TrimSpace(r.FormValue("status")))
	values := url.Values{"action": {action}}
	if returnTo := strings.TrimSpace(r.FormValue("return_to")); returnTo != "" {
		values.Set("return_to", returnTo)
	}
	http.Redirect(w, r, detailPath+"?"+values.Encode(), http.StatusFound)
}

func adminFeedbackReturnURL(raw string, reports bool) string {
	fallback := "/admin/feedback"
	if reports {
		fallback = "/admin/reports"
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != fallback {
		return fallback
	}
	return parsed.RequestURI()
}
