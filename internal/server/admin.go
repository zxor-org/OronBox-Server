package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	authcore "github.com/zxor-org/OronBox-Server/internal/auth"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

const adminCookieName = "oronbox_admin"

type adminContextKey struct{}

func (a *App) handleAdminLoginPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, "admin_login", map[string]any{
		"Title":        "管理后台",
		"AuthorizeURL": fmt.Sprintf("/oauth2/bandbbs/start?app_id=oronbox-admin&platform=web&return_uri=%s/admin", a.cfg.PublicURL),
		"Error":        strings.TrimSpace(r.URL.Query().Get("error")),
	})
}

func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("/oauth2/bandbbs/start?app_id=oronbox-admin&platform=web&return_uri=%s/admin", a.cfg.PublicURL), http.StatusFound)
}

// requireAdminRole wraps requireAdmin and additionally demands a users.role
// value, so reviewers can reach the moderation pages while account
// administration stays limited to admins.
func (a *App) requireAdminRole(role string, next http.HandlerFunc) http.HandlerFunc {
	return a.requireAdmin(func(w http.ResponseWriter, r *http.Request) {
		session := currentAdmin(r)
		user, err := a.store.UserByID(r.Context(), session.UserID)
		if err != nil || (role == "admin" && user.Role != "admin") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

func (a *App) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	_ = a.store.RecordAudit(r.Context(), actor, "admin_logout", "success", a.clientIP(r), r.UserAgent(), "")
	if cookie, err := r.Cookie(adminCookieName); err == nil {
		_ = a.store.DeleteAdminSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, a.adminSessionCookie(r, "", time.Time{}, -1))
	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// An OAuth callback delivers the login ticket as a query parameter
		// on GET /admin itself. No other admin route may use this exception.
		if isAdminOAuthReturn(r) {
			next(w, r)
			return
		}
		cookie, err := r.Cookie(adminCookieName)
		if err != nil || cookie.Value == "" {
			observability.From(r.Context()).With("component", "admin").Info(
				"admin authentication rejected",
				"reason", "missing_cookie",
			)
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		session, err := a.store.AdminSession(r.Context(), cookie.Value)
		if err != nil {
			reason := "session_lookup_failed"
			if errors.Is(err, sql.ErrNoRows) {
				reason = "session_not_found"
			}
			observability.From(r.Context()).With("component", "admin").Warn(
				"admin authentication rejected",
				"reason", reason,
			)
			a.clearAdminCookie(w, r)
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		if isUnsafeMethod(r.Method) && !a.isSameOriginAdminRequest(r) {
			_ = a.store.RecordAudit(r.Context(), session, "admin_csrf_rejected", "failure", a.clientIP(r), r.UserAgent(), "path="+r.URL.Path)
			http.Error(w, "cross-origin admin request rejected", http.StatusForbidden)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), adminContextKey{}, session)))
	}
}

func isAdminOAuthReturn(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/admin" && strings.TrimSpace(r.URL.Query().Get("ticket")) != ""
}

func isUnsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func (a *App) isSameOriginAdminRequest(r *http.Request) bool {
	expected, err := url.Parse(a.cfg.PublicURL)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	actual := strings.TrimSpace(r.Header.Get("Origin"))
	if actual == "" {
		actual = strings.TrimSpace(r.Header.Get("Referer"))
	}
	if actual == "" {
		return false
	}
	candidate, err := url.Parse(actual)
	if err != nil {
		return false
	}
	return strings.EqualFold(candidate.Scheme, expected.Scheme) && strings.EqualFold(candidate.Host, expected.Host)
}

func (a *App) secureAdminCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	publicURL, err := url.Parse(a.cfg.PublicURL)
	return err == nil && strings.EqualFold(publicURL.Scheme, "https")
}

func (a *App) clearAdminCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.adminSessionCookie(r, "", time.Time{}, -1))
}

func (a *App) adminSessionCookie(r *http.Request, value string, expiresAt time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     adminCookieName,
		Value:    value,
		Path:     "/admin",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		// The session is created during a top-level redirect from BandBBS.
		// Strict cookies may be withheld until that cross-site redirect chain
		// ends, causing the immediate /admin redirect to lose the new session.
		// Lax keeps the OAuth navigation working; unsafe admin methods still
		// require an exact Origin or Referer match.
		SameSite: http.SameSiteLaxMode,
		Secure:   a.secureAdminCookie(r),
	}
}

func currentAdmin(r *http.Request) store.AdminSession {
	session, ok := r.Context().Value(adminContextKey{}).(store.AdminSession)
	if !ok {
		panic("admin session missing from request context")
	}
	return session
}

func (a *App) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	ticket := strings.TrimSpace(r.URL.Query().Get("ticket"))
	if ticket != "" {
		user, err := a.store.ConsumeLoginTicketIdentity(r.Context(), authcore.HashToken(ticket, a.cfg.SessionSecret))
		if err != nil {
			http.Redirect(w, r, "/admin/login?error=invalid_ticket", http.StatusFound)
			return
		}
		if !containsInt64(a.cfg.Admin.BandBBSUserIDs, user.BandBBSUserID) && user.Role != "admin" && user.Role != "reviewer" {
			actor := store.AdminSession{UserID: user.ID, Username: user.Username}
			_ = a.store.RecordAudit(r.Context(), actor, "admin_login", "failure", a.clientIP(r), r.UserAgent(), "not authorized")
			http.Redirect(w, r, "/admin/login?error=not_authorized", http.StatusFound)
			return
		}
		sessionID, err := randomToken()
		if err != nil {
			http.Error(w, "failed to create session", http.StatusInternalServerError)
			return
		}
		expiresAt := time.Now().UTC().Add(12 * time.Hour)
		if err := a.store.CreateAdminSession(r.Context(), sessionID, user.ID, user.Username, a.clientIP(r), r.UserAgent(), expiresAt); err != nil {
			http.Error(w, "failed to save session", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, a.adminSessionCookie(r, sessionID, expiresAt, 0))
		actor := store.AdminSession{ID: sessionID, UserID: user.ID, Username: user.Username, ExpiresAt: expiresAt}
		_ = a.store.RecordAudit(r.Context(), actor, "admin_login", "success", a.clientIP(r), r.UserAgent(), "")
		a.renderTransition(w, web.TransitionPageData{
			Title:       "登录成功",
			Heading:     "登录成功",
			Description: "正在进入 OronBox 管理后台",
			ButtonLabel: "进入管理后台",
			Target:      template.URL("/admin"),
			Auto:        true,
			Tone:        "success",
		})
		return
	}

	stats, err := a.store.Stats(r.Context(), a.startedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	events, _ := a.store.RecentEvents(r.Context(), 12)
	clients, _ := a.store.ClientStats(r.Context(), 8)
	a.render(w, "admin_dashboard", map[string]any{
		"Title":   "仪表盘",
		"Stats":   stats,
		"Events":  events,
		"Clients": clients,
	})
}

func (a *App) handleAdminEvents(w http.ResponseWriter, r *http.Request) {
	events, err := a.store.RecentEvents(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_events", map[string]any{"Title": "OAuth 事件", "Events": events})
}

func (a *App) handleAdminStates(w http.ResponseWriter, r *http.Request) {
	states, err := a.store.ActiveStates(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_states", map[string]any{"Title": "OAuth States", "States": states})
}

func (a *App) handleAdminTickets(w http.ResponseWriter, r *http.Request) {
	tickets, err := a.store.Tickets(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_tickets", map[string]any{"Title": "OAuth Tickets", "Tickets": tickets})
}

func (a *App) handleAdminClients(w http.ResponseWriter, r *http.Request) {
	clients, err := a.store.ClientStats(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_clients", map[string]any{"Title": "客户端", "Clients": clients})
}

func (a *App) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	announcements := []store.Announcement{}
	if a.store != nil {
		var err error
		announcements, err = a.store.AdminAnnouncements(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	type oauthSettings struct {
		ClientID      string
		RedirectURI   string
		Scopes        []string
		PublishScopes []string
	}
	type settingsView struct {
		BandBBS   oauthSettings
		GitHub    oauthSettings
		PublicURL string
		Version   string
		Commit    string
	}
	a.render(w, "admin_settings", map[string]any{
		"Title": "设置",
		"Config": settingsView{
			BandBBS: oauthSettings{
				ClientID:      a.cfg.BandBBS.ClientID,
				RedirectURI:   a.cfg.BandBBS.RedirectURI,
				Scopes:        a.cfg.BandBBS.Scopes,
				PublishScopes: a.cfg.BandBBS.PublishScopes,
			},
			GitHub: oauthSettings{
				ClientID:    a.cfg.GitHub.ClientID,
				RedirectURI: a.cfg.GitHub.RedirectURI,
				Scopes:      a.cfg.GitHub.Scopes,
			},
			PublicURL: a.cfg.PublicURL,
			Version:   a.cfg.Version,
			Commit:    a.cfg.Commit,
		},
		"BandBBSSecretState": map[bool]string{true: "已配置", false: "未配置"}[a.cfg.BandBBS.ClientSecret != ""],
		"GitHubSecretState":  map[bool]string{true: "已配置", false: "未配置"}[a.cfg.GitHub.ClientSecret != ""],
		"Announcements":      announcements,
		"Action":             r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminHealth(w http.ResponseWriter, r *http.Request) {
	dbStatus := "ok"
	if err := a.store.Ping(r.Context()); err != nil {
		dbStatus = err.Error()
	}
	stats, _ := a.store.Stats(r.Context(), a.startedAt)
	a.render(w, "admin_health", map[string]any{
		"Title":    "健康",
		"DBStatus": dbStatus,
		"Stats":    stats,
	})
}

func (a *App) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	logs, err := a.store.AuditLogs(r.Context(), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_audit", map[string]any{"Title": "审计", "Logs": logs})
}

func (a *App) handleAdminCleanup(w http.ResponseWriter, r *http.Request) {
	actor := currentAdmin(r)
	stats, err := a.store.CleanupExpired(r.Context())
	result := "success"
	message := fmt.Sprintf("states=%d tickets=%d admin_sessions=%d messages=%d", stats.OAuthStates, stats.LoginTickets, stats.AdminSessions, stats.UserMessages)
	if err != nil {
		result = "failure"
		message = err.Error()
	}
	_ = a.store.RecordAudit(r.Context(), actor, "cleanup_expired", result, a.clientIP(r), r.UserAgent(), message)
	http.Redirect(w, r, "/admin/health", http.StatusFound)
}

// ---- review tab ----

type adminReviewItem struct {
	ResourceID  string
	RevisionID  string
	RevisionNo  int
	Name        string
	Summary     string
	Owner       string
	SubmittedAt string
	Targets     []string
	Snapshot    string
	Artifacts   []store.AdminArtifact
	Media       []store.AdminMedia
}

func (a *App) handleAdminReview(w http.ResponseWriter, r *http.Request) {
	queue, err := a.creator.ReviewQueue(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := make([]adminReviewItem, 0, len(queue))
	for _, workspace := range queue {
		if len(workspace.Revisions) == 0 {
			continue
		}
		entry := workspace.Revisions[0]
		names := make([]string, 0, len(workspace.Publications))
		for _, publication := range workspace.Publications {
			names = append(names, string(publication.Target))
		}
		snapshotJSON, _ := json.Marshal(workspace)
		var snapshot bytes.Buffer
		_ = json.Indent(&snapshot, snapshotJSON, "", "  ")
		detail, detailErr := a.store.AdminResource(r.Context(), workspace.Resource.ID)
		if detailErr != nil {
			http.Error(w, detailErr.Error(), http.StatusInternalServerError)
			return
		}
		items = append(items, adminReviewItem{
			ResourceID:  workspace.Resource.ID,
			RevisionID:  entry.ID,
			RevisionNo:  entry.Number,
			Name:        entry.Name,
			Summary:     entry.Summary,
			Owner:       workspace.Resource.OwnerID,
			SubmittedAt: entry.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			Targets:     names,
			Snapshot:    snapshot.String(),
			Artifacts:   detail.Artifacts,
			Media:       detail.Media,
		})
	}
	a.render(w, "admin_review", map[string]any{
		"Title":   "资源审核",
		"Items":   items,
		"Decided": r.URL.Query().Get("decided") != "",
	})
}

// handleAdminBlob exposes draft and review assets only inside an authenticated
// admin session. Public blob routes intentionally reject unpublished content.
func (a *App) handleAdminBlob(w http.ResponseWriter, r *http.Request) {
	digest := strings.TrimSpace(r.PathValue("sha256"))
	if !sha256Pattern.MatchString(digest) {
		http.NotFound(w, r)
		return
	}
	record, err := a.store.Blob(r.Context(), digest)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	reader, err := a.blobs.Open(r.Context(), record.LocalKey)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", record.MediaType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("ETag", `"`+record.SHA256+`"`)
	if r.URL.Query().Get("download") == "1" {
		name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("name")))
		if name == "" || name == "." {
			name = record.SHA256
		}
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	http.ServeContent(w, r, record.SHA256, time.Time{}, reader)
}

func (a *App) handleAdminReviewDecision(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/review", http.StatusFound)
		return
	}
	decision := r.FormValue("decision")
	if decision != "approve" && decision != "reject" {
		http.Redirect(w, r, "/admin/review", http.StatusFound)
		return
	}
	revisionID := r.PathValue("revision")
	note := strings.TrimSpace(r.FormValue("note"))
	approved := decision == "approve"
	actor := currentAdmin(r)
	if err := a.creator.Review(r.Context(), revisionID, actor.UserID, approved, note, nil); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	result := "rejected"
	if approved {
		result = "approved"
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource.review", result, a.clientIP(r), r.UserAgent(), "revision="+revisionID)
	http.Redirect(w, r, "/admin/review?decided=1", http.StatusFound)
}

func reviewItems(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		if item := strings.TrimSpace(line); item != "" {
			items = append(items, item)
		}
	}
	return items
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
	a.render(w, "admin_users", map[string]any{
		"Title":  "用户",
		"Page":   page,
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
	_ = a.store.RecordAudit(r.Context(), actor, "user."+action, "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("user=%s(%d) role=%s reason=%s", item.Username, item.BandBBSUserID, item.Role, r.FormValue("reason")))
	http.Redirect(w, r, "/admin/users?action=done", http.StatusFound)
}

func (a *App) handleAdminResources(w http.ResponseWriter, r *http.Request) {
	query := store.AdminResourceQuery{
		Search:            r.URL.Query().Get("q"),
		Owner:             r.URL.Query().Get("owner"),
		Kind:              r.URL.Query().Get("kind"),
		Moderation:        r.URL.Query().Get("moderation"),
		RevisionState:     r.URL.Query().Get("revision_state"),
		ReviewState:       r.URL.Query().Get("review_state"),
		PublicationTarget: r.URL.Query().Get("target"),
		PublicationState:  r.URL.Query().Get("publication_state"),
		Sort:              r.URL.Query().Get("sort"),
		Page:              positiveInt(r.URL.Query().Get("page"), 1),
		PerPage:           positiveInt(r.URL.Query().Get("per_page"), 25),
	}
	page, err := a.store.AdminResources(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_resources", map[string]any{
		"Title":  "资源",
		"Page":   page,
		"Items":  page.Items,
		"Query":  page.Query,
		"Action": r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminResource(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminResource(r.Context(), r.PathValue("resource"))
	if errors.Is(err, store.ErrAdminResourceNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_resource_detail", map[string]any{
		"Title":        detail.Resource.Name,
		"Item":         detail.Resource,
		"Detail":       detail,
		"Publications": detail.Publications,
		"Artifacts":    detail.Artifacts,
		"Media":        detail.Media,
		"Snapshot":     detail.Snapshot,
		"Action":       r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminResourceState(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	resourceID := r.PathValue("resource")
	action := strings.TrimSpace(r.FormValue("action"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	actor := currentAdmin(r)
	result, err := a.store.AdminManageResource(r.Context(), resourceID, action, reason, actor)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource."+action, "failure", a.clientIP(r), r.UserAgent(), "resource="+resourceID+" error="+err.Error())
		status := http.StatusConflict
		if errors.Is(err, store.ErrAdminResourceNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource."+action, "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("resource=%s slug=%s previous_moderation=%s moderation=%s reason=%s deleted=%t", result.ID, result.Slug, result.PreviousModeration, result.ModerationState, result.Reason, result.Deleted))
	observability.From(r.Context()).With("component", "admin").Info(
		"admin resource state changed",
		"resource_id", resourceID,
		"action", action,
		"admin_user", actor.Username,
		"previous_moderation", result.PreviousModeration,
		"moderation", result.ModerationState,
		"reason", result.Reason,
	)
	if result.Deleted {
		http.Redirect(w, r, "/admin/resources?action=deleted", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin/resources/"+resourceID+"?action="+action, http.StatusFound)
}

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
	a.render(w, "admin_feedback", map[string]any{
		"Title":   "反馈",
		"Page":    page,
		"Items":   page.Items,
		"Query":   page.Query,
		"Replied": r.URL.Query().Get("replied") != "",
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
	message := strings.TrimSpace(r.FormValue("message"))
	if message == "" {
		http.Redirect(w, r, "/admin/feedback", http.StatusFound)
		return
	}
	actor := currentAdmin(r)
	if _, err := a.store.ReplyFeedback(r.Context(), ticketID, actor.UserID, message, r.FormValue("close") == "yes"); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "feedback.reply", "failure", a.clientIP(r), r.UserAgent(), "ticket="+ticketID+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "feedback.reply", "success", a.clientIP(r), r.UserAgent(), "ticket="+ticketID)
	http.Redirect(w, r, "/admin/feedback?replied=1", http.StatusFound)
}

func (a *App) handleAdminReports(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminFeedback(r.Context(), store.AdminFeedbackQuery{
		Kind:         "report",
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
	a.render(w, "admin_reports", map[string]any{
		"Title":  "资源举报",
		"Page":   page,
		"Items":  page.Items,
		"Query":  page.Query,
		"Action": r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminReport(w http.ResponseWriter, r *http.Request) {
	ticket, err := a.store.Feedback(r.Context(), r.PathValue("ticket"), "", true)
	if errors.Is(err, store.ErrFeedbackNotFound) || (err == nil && ticket.Kind != "report") {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var resource *store.AdminResourceDetail
	if ticket.TargetID != "" && (ticket.TargetSource == "" || strings.EqualFold(ticket.TargetSource, "oronbox")) {
		if detail, detailErr := a.store.AdminResource(r.Context(), ticket.TargetID); detailErr == nil {
			resource = &detail
		}
	}
	a.render(w, "admin_report_detail", map[string]any{
		"Title":    ticket.Subject,
		"Item":     ticket,
		"Ticket":   ticket,
		"Resource": resource,
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
	if errors.Is(err, store.ErrFeedbackNotFound) || (err == nil && ticket.Kind != "report") {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	status := strings.TrimSpace(r.FormValue("status"))
	reply := strings.TrimSpace(r.FormValue("message"))
	if status == "" {
		if reply == "" {
			http.Error(w, "status or reply is required", http.StatusBadRequest)
			return
		}
		status = "replied"
	}
	actor := currentAdmin(r)
	updated, err := a.store.UpdateFeedback(r.Context(), ticketID, store.FeedbackUpdate{Status: status, Reply: reply, AuthorID: actor.UserID})
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "resource_report.update", "failure", a.clientIP(r), r.UserAgent(), "ticket="+ticketID+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "resource_report.update", "success", a.clientIP(r), r.UserAgent(), fmt.Sprintf("ticket=%s target=%s status=%s", ticketID, updated.TargetID, updated.Status))
	http.Redirect(w, r, "/admin/reports/"+ticketID+"?action=updated", http.StatusFound)
}

func positiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
