package server

import (
	"net/http"

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
	a.render(w, "admin_announcements", map[string]any{"Title": "公告", "Items": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/announcements", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action")})
}
