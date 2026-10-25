package server

import (
	"encoding/json"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (a *App) handleActivity(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Version  string `json:"version"`
		Platform string `json:"platform"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&input) != nil {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_activity", "invalid activity report"))
		return
	}
	input.Version = strings.TrimSpace(input.Version)
	valid := false
	for _, p := range []string{"android", "ios", "macos", "windows", "linux", "web"} {
		if input.Platform == p {
			valid = true
		}
	}
	if !valid || len(input.Version) == 0 || len(input.Version) > 120 || strings.ContainsAny(input.Version, "\r\n\t") {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_activity", "invalid version or platform"))
		return
	}
	receipt, err := a.store.RecordActivity(r.Context(), currentUser(r).ID, input.Version, input.Platform, time.Now())
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("activity_unavailable", "activity temporarily unavailable"))
		return
	}
	httpx.WriteJSON(w, 200, receipt)
}
func (a *App) handleAdminActivity(w http.ResponseWriter, r *http.Request) {
	stats, err := a.store.ActivityStats(r.Context())
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("activity_unavailable", "activity temporarily unavailable"))
		return
	}
	httpx.WriteJSON(w, 200, stats)
}
func (a *App) handleAdminActivityUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 100000 {
		page = 100000
	}
	items, total, err := a.store.ActivityUsers(r.Context(), q.Get("version"), q.Get("platform"), page)
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("activity_unavailable", "activity temporarily unavailable"))
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"items": items, "total": total, "page": page, "per_page": 30})
}
