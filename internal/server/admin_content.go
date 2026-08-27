package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminAnnouncementsPage(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminAnnouncementsPage(r.Context(), store.AdminAnnouncementQuery{Search: r.URL.Query().Get("q"), From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.render(w, r, "admin_announcements", map[string]any{"Title": "公告", "Items": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/announcements", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminAnnouncement(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("body"))
	if title == "" || body == "" {
		http.Error(w, "title and body are required", http.StatusBadRequest)
		return
	}
	if err := a.store.CreateAnnouncement(r.Context(), currentAdmin(r).UserID, title, body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "announcement.publish", "success", a.clientIP(r), r.UserAgent(), title)
	http.Redirect(w, r, "/admin/announcements?action=published", http.StatusFound)
}

func (a *App) handleAdminDeleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("announcement")
	actor := currentAdmin(r)
	if err := a.store.DeleteAnnouncement(r.Context(), id); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "announcement.delete", "success", a.clientIP(r), r.UserAgent(), "announcement="+id)
	http.Redirect(w, r, "/admin/announcements?action=deleted", http.StatusFound)
}
