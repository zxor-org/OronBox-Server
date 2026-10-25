package server

import (
	"net/http"
)

func (a *App) renderConsoleRecovery(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/console/login" {
		a.handleAdminLoginPage(w, r)
		return
	}
	status := a.consoleManager().Status(r.Context(), true)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if status.Syncing {
		a.render(w, r, "console_syncing", map[string]any{"Title": "正在同步前端"})
		return
	}
	message := status.LastError
	if message == "" {
		message = "管理后台暂不可用"
	}
	data := map[string]any{"Title": "前端同步失败", "Error": message, "ButtonLabel": "重新尝试同步", "Target": "/console/"}
	role, _ := r.Context().Value(adminRoleContextKey{}).(string)
	if role == "admin" && status.CanSync && status.Latest != nil {
		data["RetryAction"] = "/console/api/console/sync"
		data["AssetID"] = status.Latest.AssetID
	}
	a.render(w, r, "console_error", data)
}
