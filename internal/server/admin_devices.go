package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

func (a *App) handleAdminDevices(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminDevices(r.Context(), store.AdminDeviceQuery{
		Search: r.URL.Query().Get("q"), Platform: r.URL.Query().Get("platform"),
		Vendor: r.URL.Query().Get("vendor"), State: r.URL.Query().Get("state"), Sort: r.URL.Query().Get("sort"),
		Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_devices", map[string]any{
		"Title": "设备目录", "Items": page.Items, "Page": page, "Query": page.Query,
		"Pager": web.NewPagination("/admin/devices", r.URL.Query(), page.Page, page.PerPage, page.Total),
	})
}

func (a *App) handleAdminDevice(w http.ResponseWriter, r *http.Request) {
	item, err := a.store.AdminDevice(r.Context(), r.PathValue("device"))
	if errors.Is(err, store.ErrAdminDeviceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resources, err := a.store.AdminResources(r.Context(), store.AdminResourceQuery{Device: item.ID, Page: 1, PerPage: 100})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_device_detail", map[string]any{
		"Title": item.DisplayName, "Item": item, "Resources": resources.Items, "Action": r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminDeviceNew(w http.ResponseWriter, r *http.Request) {
	a.render(w, "admin_device_detail", map[string]any{
		"Title": "新增设备", "Item": store.AdminDeviceItem{Enabled: true}, "Resources": []store.AdminResourceItem{}, "New": true,
	})
}

func (a *App) handleAdminDeviceSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.PathValue("device"))
	if id == "new" {
		id = ""
	}
	input := store.AdminDeviceInput{
		ID: id, DisplayName: r.FormValue("display_name"), Codename: r.FormValue("codename"),
		Platform: r.FormValue("platform"), Vendor: r.FormValue("vendor"), AstroBoxID: r.FormValue("astrobox_id"),
		Enabled: r.FormValue("enabled") == "on",
	}
	actor := currentAdmin(r)
	item, err := a.store.AdminSaveDevice(r.Context(), input)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "device.save", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "device.save", "success", a.clientIP(r), r.UserAgent(), "device="+item.ID+" codename="+item.Codename)
	http.Redirect(w, r, "/admin/devices/"+item.ID+"?action=saved", http.StatusFound)
}
