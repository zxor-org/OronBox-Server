package server

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/consoleupdate"
	"github.com/zxor-org/OronBox-Server/internal/httpx"
	adminhttp "github.com/zxor-org/OronBox-Server/internal/server/admin"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func (a *App) handleAdminAPISession(w http.ResponseWriter, r *http.Request) {
	session := currentAdmin(r)
	role, _ := r.Context().Value(adminRoleContextKey{}).(string)
	user, err := a.store.UserByID(r.Context(), session.UserID)
	if err != nil {
		httpx.WriteJSON(w, 503, httpx.ErrorBody("session_unavailable", "account lookup temporarily unavailable"))
		return
	}
	w.Header().Set("Cache-Control", "no-store")

	pending, err := a.store.AdminReviews(r.Context(), store.AdminReviewQuery{State: "pending", Page: 1, PerPage: 1})
	if err != nil {
		httpx.WriteJSON(w, http.StatusInternalServerError, httpx.ErrorBody("session_failed", err.Error()))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"user": user.Username, "bandbbs_uid": user.BandBBSUserID, "avatar_url": user.AvatarURL, "expires_in": max(0, int(time.Until(session.ExpiresAt).Seconds())),
		"refresh_in":          max(0, int(time.Until(session.RotatedAt.Add(11*time.Hour)).Seconds())),
		"absolute_expires_at": session.AbsoluteExpiresAt, "expires_at": session.ExpiresAt, "server_time": time.Now().UTC(),
		"authenticated": true, "username": user.Username, "uid": session.UserID,
		"role":            role,
		"csrf_token":      a.adminCSRFTokenFor(r),
		"pending_reviews": pending.Total,
	})
}

func (a *App) adminHandlers() *adminhttp.Handler {
	a.adminOnce.Do(func() {
		a.admin = adminhttp.New(adminhttp.Dependencies{Config: a.cfg, Store: a.store, Creator: a.creator, Actor: currentAdmin, CSRFToken: a.adminCSRFTokenFor, ClientIP: a.clientIP, Audit: a.audit})
	})
	return a.admin
}

func (a *App) consoleManager() *consoleupdate.Manager {
	a.consoleOnce.Do(func() {
		root := a.cfg.ConsoleDirectory
		if root == "" {
			root = filepath.Join(filepath.Dir(a.cfg.Storage.LocalRoot), "console")
		}
		a.console = consoleupdate.New(consoleupdate.Options{Directory: root, Source: a.cfg.DeviceCatalog, Repository: a.cfg.ConsoleRepository, ContentSecurityPolicy: contentCSP(a.cfg)})
	})
	return a.console
}

func (a *App) serveConsole(w http.ResponseWriter, r *http.Request) {
	if !a.consoleManager().Installed() && !strings.HasPrefix(r.URL.Path, "/console/assets/") && r.URL.Path != "/console/favicon.svg" {
		a.renderConsoleRecovery(w, r)
		return
	}
	a.consoleManager().ServeHTTP(w, r)
}

func (a *App) handleConsoleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, 200, a.consoleManager().Status(r.Context(), r.URL.Query().Get("refresh") == "1"))
}

func (a *App) handleConsoleSync(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AssetID int64 `json:"asset_id"`
	}
	form := strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded")
	var decodeErr error
	if form {
		decodeErr = r.ParseForm()
		in.AssetID, _ = strconv.ParseInt(r.PostForm.Get("asset_id"), 10, 64)
	} else {
		decodeErr = httpx.DecodeJSON(r, &in)
	}
	if decodeErr != nil || in.AssetID <= 0 {
		httpx.WriteJSON(w, 400, httpx.ErrorBody("invalid_request", "reviewed asset_id is required"))
		return
	}
	current, err := a.consoleManager().Install(r.Context(), in.AssetID)
	if errors.Is(err, consoleupdate.ErrBusy) {
		httpx.WriteJSON(w, 409, httpx.ErrorBody("console_busy", err.Error()))
		return
	}
	if err != nil {
		if form {
			a.renderConsoleRecovery(w, r)
			return
		}
		httpx.WriteJSON(w, 409, httpx.ErrorBody("console_sync_failed", err.Error()))
		return
	}
	a.audit(r, "console.install", "success", fmt.Sprintf("asset=%d", in.AssetID))
	if form {
		http.Redirect(w, r, "/console/", http.StatusSeeOther)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"synced": true, "metadata": current})
}
