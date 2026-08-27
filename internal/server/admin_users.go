package server

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
	"strings"
)

func userDetailPage(r *http.Request, name string) store.AdminUserDetailPageQuery {
	return store.AdminUserDetailPageQuery{Page: positiveInt(r.URL.Query().Get(name+"_page"), 1), PerPage: positiveInt(r.URL.Query().Get(name+"_per_page"), 25)}
}

func (a *App) handleAdminUserDetail(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminUserDetail(r.Context(), r.PathValue("user"), store.AdminUserDetailQuery{Resources: userDetailPage(r, "resources"), Comments: userDetailPage(r, "comments"), Tickets: userDetailPage(r, "tickets"), Messages: userDetailPage(r, "messages"), Ledger: userDetailPage(r, "ledger"), Sessions: userDetailPage(r, "sessions"), Audit: userDetailPage(r, "audit")})
	if errors.Is(err, store.ErrAdminUserNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pager := func(name string, p, size, total int) map[string]any {
		return map[string]any{"Pager": web.NewNamedPagination("/admin/users/"+r.PathValue("user"), r.URL.Query(), p, size, total, name+"_page", name+"_per_page"), "PageSizes": []int{25, 50, 100}}
	}
	a.render(w, r, "admin_user_workspace", map[string]any{"Title": detail.User.Username, "Detail": detail, "ResourcesPager": pager("resources", detail.Resources.Page, detail.Resources.PerPage, detail.Resources.Total), "CommentsPager": pager("comments", detail.Comments.Page, detail.Comments.PerPage, detail.Comments.Total), "TicketsPager": pager("tickets", detail.Tickets.Page, detail.Tickets.PerPage, detail.Tickets.Total), "MessagesPager": pager("messages", detail.Messages.Page, detail.Messages.PerPage, detail.Messages.Total), "LedgerPager": pager("ledger", detail.Ledger.Page, detail.Ledger.PerPage, detail.Ledger.Total), "SessionsPager": pager("sessions", detail.Sessions.Page, detail.Sessions.PerPage, detail.Sessions.Total), "AuditPager": pager("audit", detail.Audit.Page, detail.Audit.PerPage, detail.Audit.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminUserSessions(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actor, action := currentAdmin(r), r.FormValue("action")
	var err error
	detail := "user=" + r.PathValue("user")
	if action == "revoke_all" {
		var count int64
		count, err = a.store.AdminRevokeAllUserSessions(r.Context(), r.PathValue("user"))
		detail += " count=" + fmt.Sprint(count)
	} else {
		err = a.store.AdminRevokeUserSession(r.Context(), r.PathValue("user"), r.FormValue("session_id"))
		detail += " session=" + r.FormValue("session_id")
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "user.sessions.revoke", "failure", a.clientIP(r), r.UserAgent(), detail+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "user.sessions.revoke", "success", a.clientIP(r), r.UserAgent(), detail)
	http.Redirect(w, r, "/admin/users/"+r.PathValue("user")+"?action=sessions_revoked", http.StatusFound)
}

func (a *App) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminUsers(r.Context(), store.AdminUserQuery{
		Search:  r.URL.Query().Get("q"),
		Page:    positiveInt(r.URL.Query().Get("page"), 1),
		PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "admin_users", map[string]any{
		"Title":  "用户",
		"Page":   page,
		"Pager":  web.NewPagination("/admin/users", r.URL.Query(), page.Page, page.PerPage, page.Total),
		"Items":  page.Items,
		"Query":  page.Query,
		"Action": r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminUserState(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	userID := r.PathValue("user")
	action := strings.TrimSpace(r.FormValue("action"))
	actor := currentAdmin(r)
	item, err := a.store.AdminManageUser(r.Context(), userID, action, r.FormValue("reason"), r.FormValue("role"), actor)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "user."+action, "failure", a.clientIP(r), r.UserAgent(), "user="+userID+" error="+err.Error())
		status := http.StatusConflict
		if errors.Is(err, store.ErrAdminUserNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "user."+action, "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("user_id=%s user=%s(%d) role=%s reason=%s", userID, item.Username, item.BandBBSUserID, item.Role, r.FormValue("reason")))
	http.Redirect(w, r, "/admin/users?action=done", http.StatusFound)
}
