package server

import (
	"fmt"
	"net/http"
)

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
