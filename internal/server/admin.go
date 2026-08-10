package server

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/csv"
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
	"github.com/zxor-org/OronBox-Server/internal/creator"
	"github.com/zxor-org/OronBox-Server/internal/observability"
	"github.com/zxor-org/OronBox-Server/internal/store"
	"github.com/zxor-org/OronBox-Server/internal/web"
)

const adminCookieName = "oronbox_admin"

type adminContextKey struct{}
type adminRoleContextKey struct{}

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
		currentRole, _ := r.Context().Value(adminRoleContextKey{}).(string)
		if role == "admin" && currentRole != "admin" {
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
		user, err := a.store.UserByID(r.Context(), session.UserID)
		if err != nil || (user.Role != "admin" && user.Role != "reviewer" && !containsInt64(a.cfg.Admin.BandBBSUserIDs, user.BandBBSUserID)) {
			observability.From(r.Context()).With("component", "admin").Warn(
				"admin authorization rejected",
				"reason", "role_revoked",
			)
			a.clearAdminCookie(w, r)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if isUnsafeMethod(r.Method) && !a.isSameOriginAdminRequest(r) {
			_ = a.store.RecordAudit(r.Context(), session, "admin_csrf_rejected", "failure", a.clientIP(r), r.UserAgent(), "path="+r.URL.Path)
			http.Error(w, "cross-origin admin request rejected", http.StatusForbidden)
			return
		}
		ctx := context.WithValue(r.Context(), adminContextKey{}, session)
		ctx = context.WithValue(ctx, adminRoleContextKey{}, user.Role)
		next(w, r.WithContext(ctx))
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
			Title:       "授权完成",
			Heading:     "授权完成",
			Description: "可以返回 OronBox 继续使用",
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
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthEvents(r.Context(), store.AdminOAuthEventQuery{
		Search: r.URL.Query().Get("q"), App: r.URL.Query().Get("app"), Result: r.URL.Query().Get("result"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_events", map[string]any{"Title": "OAuth 事件", "Events": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/oauth/events", r.URL.Query(), page.Page, page.PerPage, page.Total)})
}

func (a *App) handleAdminEvent(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("event"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	detail, err := a.store.AdminOAuthEvent(r.Context(), id)
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_event_detail", map[string]any{"Title": "OAuth 事件详情", "Detail": detail})
}

func (a *App) handleAdminStates(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthStates(r.Context(), store.AdminOAuthStateQuery{
		Search: r.URL.Query().Get("q"), App: r.URL.Query().Get("app"), Status: r.URL.Query().Get("status"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_states", map[string]any{"Title": "OAuth States", "States": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/oauth/states", r.URL.Query(), page.Page, page.PerPage, page.Total)})
}

func (a *App) handleAdminState(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminOAuthState(r.Context(), r.PathValue("state"))
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_state_detail", map[string]any{"Title": "OAuth State 详情", "Detail": detail})
}

func (a *App) handleAdminTickets(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminOAuthTickets(r.Context(), store.AdminOAuthTicketQuery{
		Search: r.URL.Query().Get("q"), App: r.URL.Query().Get("app"), Status: r.URL.Query().Get("status"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_tickets", map[string]any{"Title": "OAuth Tickets", "Tickets": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/oauth/tickets", r.URL.Query(), page.Page, page.PerPage, page.Total)})
}

func (a *App) handleAdminTicket(w http.ResponseWriter, r *http.Request) {
	detail, err := a.store.AdminOAuthTicket(r.Context(), r.PathValue("ticket"))
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_ticket_detail", map[string]any{"Title": "登录 Ticket 详情", "Detail": detail})
}

func (a *App) handleAdminClients(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminClientStats(r.Context(), store.AdminClientStatsQuery{
		Search: r.URL.Query().Get("q"), App: r.URL.Query().Get("app"), Result: r.URL.Query().Get("result"), Platform: r.URL.Query().Get("platform"),
		From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_clients", map[string]any{"Title": "客户端统计", "Clients": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/clients", r.URL.Query(), page.Page, page.PerPage, page.Total)})
}

func (a *App) handleAdminClient(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	detail, err := a.store.AdminClient(r.Context(), r.URL.Query().Get("app"), r.URL.Query().Get("version"), r.URL.Query().Get("build"), r.URL.Query().Get("platform"), store.AdminOAuthEventQuery{Result: r.URL.Query().Get("result"), From: from, To: to, Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if errors.Is(err, store.ErrAdminDiagnosticNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_client_detail", map[string]any{"Title": "客户端详情", "Detail": detail, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/clients/detail", r.URL.Query(), detail.Events.Page, detail.Events.PerPage, detail.Events.Total)})
}

func (a *App) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	announcements := []store.Announcement{}
	attributes := []creator.ResourceAttribute{}
	if a.store != nil {
		var err error
		announcements, err = a.store.AdminAnnouncements(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if a.creator != nil {
		var err error
		attributes, err = a.creator.Attributes(r.Context(), true)
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
		"ResourceAttributes": attributes,
		"Action":             r.URL.Query().Get("action"),
	})
}

func (a *App) handleAdminResourceAttribute(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/admin/settings", http.StatusFound)
		return
	}
	coefficient, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("coefficient")), 64)
	if err != nil {
		http.Error(w, "invalid coefficient", http.StatusBadRequest)
		return
	}
	position, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("position")))
	item := creator.ResourceAttribute{
		ID: strings.TrimSpace(r.FormValue("id")), NameZH: r.FormValue("name_zh"), NameEN: r.FormValue("name_en"),
		Coefficient: coefficient, Enabled: r.FormValue("enabled") == "on", Position: position,
	}
	if err := a.creator.UpsertAttribute(r.Context(), item); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "resource_attribute.save", "success", a.clientIP(r), r.UserAgent(), item.ID)
	http.Redirect(w, r, "/admin/settings?action=attribute_saved", http.StatusFound)
}

func (a *App) handleAdminDeleteResourceAttribute(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("attribute"))
	if err := a.creator.DisableAttribute(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = a.store.RecordAudit(r.Context(), currentAdmin(r), "resource_attribute.disable", "success", a.clientIP(r), r.UserAgent(), id)
	http.Redirect(w, r, "/admin/settings?action=attribute_deleted", http.StatusFound)
}

func (a *App) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	query := adminAuditQuery(r)
	page, err := a.store.AdminAuditLogs(r.Context(), query)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	exportQuery := r.URL.Query()
	exportQuery.Del("page")
	exportQuery.Del("per_page")
	exportURL := "/admin/audit.csv"
	if encoded := exportQuery.Encode(); encoded != "" {
		exportURL += "?" + encoded
	}
	a.render(w, "admin_audit", map[string]any{"Title": "审计日志", "Logs": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "ExportURL": exportURL, "Pager": web.NewPagination("/admin/audit", r.URL.Query(), page.Page, page.PerPage, page.Total)})
}

func adminAuditQuery(r *http.Request) store.AdminAuditLogQuery {
	from, to := adminTimeRange(r.URL.Query())
	return store.AdminAuditLogQuery{
		Search: r.URL.Query().Get("q"), Result: r.URL.Query().Get("result"), TargetType: r.URL.Query().Get("target_type"), TargetID: r.URL.Query().Get("target_id"), ActorUserID: r.URL.Query().Get("actor_user_id"), From: from, To: to,
		Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	}
}

func (a *App) handleAdminAuditDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("audit"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	item, err := a.store.AdminAuditLog(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_audit_detail", map[string]any{"Title": "审计详情", "Item": item})
}

func (a *App) handleAdminAuditCSV(w http.ResponseWriter, r *http.Request) {
	items, err := a.store.AdminAuditLogsForExport(r.Context(), adminAuditQuery(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="oronbox-audit.csv"`)
	_, _ = w.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"id", "created_at", "actor_user_id", "username", "action", "result", "ip", "user_agent", "message", "target_type", "target_id", "before", "after", "metadata"})
	for _, item := range items {
		row := []string{strconv.FormatInt(item.ID, 10), item.CreatedAt, item.ActorUserID, item.Username, item.Action, item.Result, item.IP, item.UserAgent, item.Message, item.Target.Type, item.Target.ID, auditCSVJSON(item.Before), auditCSVJSON(item.After), auditCSVJSON(item.Metadata)}
		for index := range row {
			row[index] = safeCSVCell(row[index])
		}
		_ = writer.Write(row)
	}
	writer.Flush()
}

func safeCSVCell(value string) string {
	if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
		return "'" + value
	}
	return value
}

func auditCSVJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil || string(raw) == "null" {
		return ""
	}
	return string(raw)
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
	Attributes  []string
}

func (a *App) handleAdminReview(w http.ResponseWriter, r *http.Request) {
	from, to := adminTimeRange(r.URL.Query())
	page, err := a.store.AdminReviews(r.Context(), store.AdminReviewQuery{Search: r.URL.Query().Get("q"), Kind: r.URL.Query().Get("kind"), Target: r.URL.Query().Get("target"), Owner: r.URL.Query().Get("owner"), State: r.URL.Query().Get("state"), From: from, To: to, Sort: r.URL.Query().Get("sort"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reviewers, err := a.store.AdminReviewers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_review", map[string]any{
		"Title": "审核中心", "Items": page.Items, "Page": page, "Query": page.Query, "Pager": web.NewPagination("/admin/review", r.URL.Query(), page.Page, page.PerPage, page.Total), "Decided": r.URL.Query().Get("decided") != "", "BulkDone": r.URL.Query().Get("bulk") != "", "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Reviewers": reviewers, "ReturnTo": r.URL.RequestURI(),
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
	actor := currentAdmin(r)
	_ = a.store.RecordAuditData(r.Context(), actor, "blob.read", "success", a.clientIP(r), r.UserAgent(), adminBlobReadAuditData(record.SHA256, r.URL.Query().Get("download") == "1"))
	http.ServeContent(w, r, record.SHA256, time.Time{}, reader)
}

func adminBlobReadAuditData(sha256 string, download bool) store.AuditData {
	return store.AuditData{
		Message:  fmt.Sprintf("sha256=%s download=%t", sha256, download),
		Target:   store.AuditTarget{Type: "blob", ID: sha256, Label: "sensitive blob read"},
		Metadata: map[string]any{"download": download, "sensitive": true},
	}
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
	grade := strings.TrimSpace(r.FormValue("curation_grade"))
	if grade == "" {
		grade = "standard"
	}
	approved := decision == "approve"
	actor := currentAdmin(r)
	if err := a.creator.Review(r.Context(), revisionID, actor.UserID, approved, note, reviewItems(r.FormValue("items")), r.Form["attributes"], grade); err != nil {
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

func (a *App) handleAdminCollectionReviewQueue(w http.ResponseWriter, r *http.Request) {
	items, err := a.creator.CollectionReviewQueue(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorBody("collection_review_failed", err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"collections": items})
}

func (a *App) handleAdminCollectionReviewDecision(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Approve bool   `json:"approve"`
		Note    string `json:"note"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody("invalid_request", err.Error()))
		return
	}
	actor := currentAdmin(r)
	err := a.creator.ReviewCollection(r.Context(), r.PathValue("revision"), actor.UserID, request.Approve, request.Note)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		writeJSON(w, http.StatusConflict, errorBody("collection_review_failed", err.Error()))
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "success", a.clientIP(r), r.UserAgent(), "revision="+r.PathValue("revision"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleAdminCollectionsPage(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminCollections(r.Context(), store.AdminCollectionQuery{
		Search: r.URL.Query().Get("q"), Owner: r.URL.Query().Get("owner"), Kind: r.URL.Query().Get("kind"),
		State: r.URL.Query().Get("state"), Sort: r.URL.Query().Get("sort"),
		Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_collections", map[string]any{"Title": "合集管理", "Items": page.Items, "Page": page, "Query": page.Query, "Pager": web.NewPagination("/admin/collections", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminCollectionReviewForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	approve := r.FormValue("decision") == "approve"
	err := a.creator.ReviewCollection(r.Context(), r.PathValue("revision"), actor.UserID, approve, r.FormValue("note"))
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "collection.review", "success", a.clientIP(r), r.UserAgent(), "revision="+r.PathValue("revision"))
	http.Redirect(w, r, "/admin/collections?action=reviewed", http.StatusFound)
}

func (a *App) handleAdminPluginsPage(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminPluginsV2(r.Context(), store.AdminPluginQuery{Search: r.URL.Query().Get("q"), State: r.URL.Query().Get("state"), Uploader: r.URL.Query().Get("uploader"), Runtime: r.URL.Query().Get("runtime"), Sort: r.URL.Query().Get("sort"), Page: positiveInt(r.URL.Query().Get("page"), 1), PerPage: positiveInt(r.URL.Query().Get("per_page"), 25)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_plugins", map[string]any{"Title": "插件管理", "Items": page.Items, "Page": page, "Query": page.Query, "Pager": web.NewPagination("/admin/plugins", r.URL.Query(), page.Page, page.PerPage, page.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminPluginReview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	pluginID := r.PathValue("plugin")
	decision := strings.TrimSpace(r.FormValue("decision"))
	note := strings.TrimSpace(r.FormValue("note"))
	var state, reason string
	switch decision {
	case "approve":
		state = "listed"
	case "reject":
		if note == "" {
			http.Error(w, "reject reason is required", http.StatusBadRequest)
			return
		}
		state, reason = "rejected", note
	default:
		http.Error(w, "unknown decision", http.StatusBadRequest)
		return
	}
	pluginDetail, err := a.store.AdminPluginV2(r.Context(), pluginID)
	if errors.Is(err, store.ErrPluginNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pluginDetail.Plugin.PendingVersionID == "" {
		http.Error(w, "plugin is not pending review", http.StatusConflict)
		return
	}
	if _, err := a.store.SetPluginState(r.Context(), pluginID, state, reason); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "failure", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin.review", "success", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" decision="+decision+" note="+note)
	observability.From(r.Context()).With("component", "admin").Info(
		"admin plugin reviewed",
		"plugin_id", pluginID,
		"decision", decision,
		"admin_user", actor.Username,
		"reason", note,
	)
	http.Redirect(w, r, "/admin/plugins?action=reviewed", http.StatusFound)
}

func (a *App) handleAdminPluginState(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	pluginID := r.PathValue("plugin")
	action := strings.TrimSpace(r.FormValue("action"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	var state, from string
	switch action {
	case "delist":
		if reason == "" {
			http.Error(w, "delist reason is required", http.StatusBadRequest)
			return
		}
		state, from = "delisted", "listed"
	case "restore":
		state, from, reason = "listed", "delisted", ""
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}
	plugin, err := a.store.Plugin(r.Context(), pluginID)
	if errors.Is(err, store.ErrPluginNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if plugin.State != from {
		http.Error(w, "plugin state does not allow this action", http.StatusConflict)
		return
	}
	if _, err := a.store.SetPluginState(r.Context(), pluginID, state, reason); err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "plugin."+action, "failure", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "plugin."+action, "success", a.clientIP(r), r.UserAgent(), "plugin="+pluginID+" reason="+reason)
	observability.From(r.Context()).With("component", "admin").Info(
		"admin plugin state changed",
		"plugin_id", pluginID,
		"action", action,
		"admin_user", actor.Username,
		"reason", reason,
	)
	http.Redirect(w, r, "/admin/plugins?action="+action, http.StatusFound)
}

func (a *App) handleAdminCoinsPage(w http.ResponseWriter, r *http.Request) {
	stats, err := a.store.AdminCoinStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	page, perPage := positiveInt(r.URL.Query().Get("page"), 1), positiveInt(r.URL.Query().Get("per_page"), 25)
	if perPage > 100 {
		perPage = 100
	}
	from, to := adminTimeRange(r.URL.Query())
	ledger, err := a.store.AdminCoinLedgerPage(r.Context(), store.AdminCoinQuery{Search: r.URL.Query().Get("q"), User: r.URL.Query().Get("user"), Kind: r.URL.Query().Get("kind"), ReferenceType: r.URL.Query().Get("reference_type"), Sort: r.URL.Query().Get("sort"), From: from, To: to, Page: page, PerPage: perPage})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	users, err := a.store.AdminCoinUserOptions(r.Context(), r.URL.Query().Get("user_search"), 30)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_coins", map[string]any{"Title": "硬币管理", "Stats": stats, "Ledger": ledger.Items, "Page": ledger, "Query": ledger.Query, "Users": users, "Pager": web.NewPagination("/admin/coins", r.URL.Query(), ledger.Page, ledger.PerPage, ledger.Total), "Action": r.URL.Query().Get("action")})
}

func (a *App) handleAdminCoinUserForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	userID, action, reason := strings.TrimSpace(r.FormValue("user_id")), r.FormValue("action"), strings.TrimSpace(r.FormValue("reason"))
	var err error
	switch action {
	case "adjust":
		var delta int64
		delta, err = strconv.ParseInt(r.FormValue("delta_units"), 10, 64)
		if err == nil {
			_, err = a.store.AdminAdjustCoins(r.Context(), userID, delta, reason, actor.UserID)
		}
	case "freeze":
		err = a.store.AdminSetCoinFreeze(r.Context(), userID, true, reason)
	case "unfreeze":
		err = a.store.AdminSetCoinFreeze(r.Context(), userID, false, reason)
	default:
		err = fmt.Errorf("unknown coin action")
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "coins."+action, "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "coins."+action, "success", a.clientIP(r), r.UserAgent(), "user="+userID+" reason="+reason)
	http.Redirect(w, r, "/admin/coins?action="+action, http.StatusFound)
}

func (a *App) handleAdminCoinInvalidateForm(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	actor := currentAdmin(r)
	err := a.store.AdminInvalidateCoinVote(r.Context(), r.FormValue("resource_id"), r.FormValue("user_id"), r.FormValue("reason"), actor.UserID)
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, "coins.invalidate", "failure", a.clientIP(r), r.UserAgent(), err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, "coins.invalidate", "success", a.clientIP(r), r.UserAgent(), "resource="+r.FormValue("resource_id")+" user="+r.FormValue("user_id"))
	http.Redirect(w, r, "/admin/coins?action=invalidated", http.StatusFound)
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
		"Pager":  web.NewPagination("/admin/resources", r.URL.Query(), page.Page, page.PerPage, page.Total),
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
		"Title":    "反馈",
		"Page":     page,
		"Pager":    web.NewPagination("/admin/feedback", r.URL.Query(), page.Page, page.PerPage, page.Total),
		"Items":    page.Items,
		"Query":    page.Query,
		"Replied":  r.URL.Query().Get("replied") != "",
		"ReturnTo": r.URL.RequestURI(),
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
	actor := currentAdmin(r)
	a.handleAdminFeedbackMutation(w, r, ticket, actor, "/admin/feedback/"+ticketID)
}

func (a *App) handleAdminFeedbackDetail(w http.ResponseWriter, r *http.Request) {
	a.renderAdminFeedbackDetail(w, r, false)
}

func (a *App) handleAdminReports(w http.ResponseWriter, r *http.Request) {
	page, err := a.store.AdminFeedback(r.Context(), store.AdminFeedbackQuery{
		Kind:         store.FeedbackKindReports,
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
		"Title":    "举报",
		"Page":     page,
		"Pager":    web.NewPagination("/admin/reports", r.URL.Query(), page.Page, page.PerPage, page.Total),
		"Items":    page.Items,
		"Query":    page.Query,
		"Action":   r.URL.Query().Get("action"),
		"ReturnTo": r.URL.RequestURI(),
	})
}

func (a *App) handleAdminReport(w http.ResponseWriter, r *http.Request) {
	a.renderAdminFeedbackDetail(w, r, true)
}

func (a *App) renderAdminFeedbackDetail(w http.ResponseWriter, r *http.Request, reportsOnly bool) {
	detail, err := a.store.AdminFeedbackDetail(r.Context(), r.PathValue("ticket"))
	if errors.Is(err, store.ErrFeedbackNotFound) || (err == nil && reportsOnly && !store.IsReportKind(detail.Ticket.Kind)) || (err == nil && !reportsOnly && detail.Ticket.Kind != store.FeedbackKindFeedback) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin_report_detail", map[string]any{
		"Title":    detail.Ticket.Subject,
		"Item":     detail.Ticket,
		"Ticket":   detail.Ticket,
		"Detail":   detail,
		"IsReport": store.IsReportKind(detail.Ticket.Kind),
		"BackURL":  adminFeedbackReturnURL(r.URL.Query().Get("return_to"), reportsOnly),
		"ReturnTo": r.URL.Query().Get("return_to"),
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
	if errors.Is(err, store.ErrFeedbackNotFound) || (err == nil && !store.IsReportKind(ticket.Kind)) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	actor := currentAdmin(r)
	a.handleAdminFeedbackMutation(w, r, ticket, actor, "/admin/reports/"+ticketID)
}

func (a *App) handleAdminFeedbackMutation(w http.ResponseWriter, r *http.Request, ticket store.FeedbackTicket, actor store.AdminSession, detailPath string) {
	action := strings.TrimSpace(r.FormValue("action"))
	if action == "" {
		action = "public_reply"
	}
	var err error
	auditAction := "feedback." + action
	switch action {
	case "internal_note":
		_, err = a.store.AddFeedbackInternalNote(r.Context(), ticket.ID, actor.UserID, r.FormValue("internal_note"))
	case "public_reply", "status":
		status := strings.TrimSpace(r.FormValue("status"))
		message := strings.TrimSpace(r.FormValue("message"))
		if status == "" {
			status = ticket.Status
			if action == "public_reply" && (status == "open" || status == "investigating") {
				status = "replied"
			}
		}
		if action == "public_reply" && message == "" {
			err = errors.New("reply is required")
		} else if action == "status" && status == ticket.Status {
			err = errors.New("select a different status")
		} else {
			_, err = a.store.UpdateFeedback(r.Context(), ticket.ID, store.FeedbackUpdate{Status: status, Reply: message, AuthorID: actor.UserID})
		}
	default:
		err = errors.New("invalid feedback action")
	}
	if err != nil {
		_ = a.store.RecordAudit(r.Context(), actor, auditAction, "failure", a.clientIP(r), r.UserAgent(), "ticket="+ticket.ID+" ticket_kind="+ticket.Kind+" error="+err.Error())
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	_ = a.store.RecordAudit(r.Context(), actor, auditAction, "success", a.clientIP(r), r.UserAgent(), "ticket="+ticket.ID+" ticket_kind="+ticket.Kind+" status="+strings.TrimSpace(r.FormValue("status")))
	values := url.Values{"action": {action}}
	if returnTo := strings.TrimSpace(r.FormValue("return_to")); returnTo != "" {
		values.Set("return_to", returnTo)
	}
	http.Redirect(w, r, detailPath+"?"+values.Encode(), http.StatusFound)
}

func adminFeedbackReturnURL(raw string, reports bool) string {
	fallback := "/admin/feedback"
	if reports {
		fallback = "/admin/reports"
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != fallback {
		return fallback
	}
	return parsed.RequestURI()
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
