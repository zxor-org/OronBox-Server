package server

import (
	"net/http"
	"strings"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

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
