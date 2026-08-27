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
	"net/http"
	"net/url"
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
	a.render(w, r, "admin_login", map[string]any{
		"Title":        "管理后台",
		"AuthorizeURL": fmt.Sprintf("/oauth2/bandbbs/start?app_id=oronbox-admin&platform=web&return_uri=%s/admin", a.cfg.PublicURL),
		"Error":        adminLoginErrorMessage(r.URL.Query().Get("error")),
	})
}

// adminLoginErrorMessage maps the codes this server emits onto fixed copy. The
// query parameter is attacker-controlled, and reflecting it verbatim would let
// anyone put their own text on the administrator login page.
func adminLoginErrorMessage(code string) string {
	switch strings.TrimSpace(code) {
	case "":
		return ""
	case "invalid_ticket":
		return "登录票据无效或已过期，请重新登录"
	case "not_authorized":
		return "该账号没有管理后台权限"
	default:
		return "登录未能完成，请重新尝试"
	}
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
		if role == web.RoleAdmin && currentRole != web.RoleAdmin {
			a.renderForbidden(w, r)
			return
		}
		next(w, r)
	})
}

// renderForbidden explains the boundary inside the console instead of dropping
// the reviewer onto a bare "forbidden" page. A GET lands on a real page, but a
// blocked write still gets a plain status because it came from a form post
// whose result nobody is going to read as a document.
func (a *App) renderForbidden(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "当前账号没有执行该操作的权限", http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusForbidden)
	a.render(w, r, "admin_forbidden", map[string]any{
		"Title": "无权访问",
		"Path":  r.URL.Path,
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
		if isUnsafeMethod(r.Method) {
			// Two independent checks. The Origin match stops a plain
			// cross-site form post; the session-bound token additionally
			// survives a browser or proxy that strips those headers, and
			// stops a same-origin injection from forging a request.
			reason := ""
			switch {
			case !a.isSameOriginAdminRequest(r):
				reason = "cross_origin"
			case isMultipartUpload(r):
				// Deferred to parseAdminUpload, which knows the route's size
				// budget. Reading the body here would bypass that cap.
			case !a.adminCSRFAccepted(r, session.ID):
				reason = "missing_csrf_token"
			}
			if reason != "" {
				_ = a.store.RecordAudit(r.Context(), session, "admin_csrf_rejected", "failure", a.clientIP(r), r.UserAgent(), "path="+r.URL.Path+" reason="+reason)
				http.Error(w, "admin request rejected", http.StatusForbidden)
				return
			}
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
		a.renderTransition(w, r, web.TransitionPageData{
			Title:       "授权完成",
			Heading:     "授权完成",
			Description: "可以返回 OronBox 继续使用",
			// A reviewer's work starts at the queue; sending them to a platform
			// dashboard they cannot act on just costs them a click.
			Target: template.URL(web.HomePathFor(user.Role)),
			Auto:   true,
			Tone:   "success",
		})
		return
	}

	if role, _ := r.Context().Value(adminRoleContextKey{}).(string); role != web.RoleAdmin {
		http.Redirect(w, r, web.HomePathFor(role), http.StatusFound)
		return
	}
	stats, err := a.store.Stats(r.Context(), a.startedAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	events, _ := a.store.RecentEvents(r.Context(), 12)
	clients, _ := a.store.ClientStats(r.Context(), 8)
	a.render(w, r, "admin_dashboard", map[string]any{
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
	a.render(w, r, "admin_events", map[string]any{"Title": "OAuth 事件", "Events": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/oauth/events", r.URL.Query(), page.Page, page.PerPage, page.Total)})
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
	a.render(w, r, "admin_event_detail", map[string]any{"Title": "OAuth 事件详情", "Detail": detail})
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
	a.render(w, r, "admin_states", map[string]any{"Title": "OAuth States", "States": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/oauth/states", r.URL.Query(), page.Page, page.PerPage, page.Total)})
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
	a.render(w, r, "admin_state_detail", map[string]any{"Title": "OAuth State 详情", "Detail": detail})
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
	a.render(w, r, "admin_tickets", map[string]any{"Title": "OAuth Tickets", "Tickets": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/oauth/tickets", r.URL.Query(), page.Page, page.PerPage, page.Total)})
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
	a.render(w, r, "admin_ticket_detail", map[string]any{"Title": "登录 Ticket 详情", "Detail": detail})
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
	a.render(w, r, "admin_clients", map[string]any{"Title": "客户端统计", "Clients": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/clients", r.URL.Query(), page.Page, page.PerPage, page.Total)})
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
	a.render(w, r, "admin_client_detail", map[string]any{"Title": "客户端详情", "Detail": detail, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "Pager": web.NewPagination("/admin/clients/detail", r.URL.Query(), detail.Events.Page, detail.Events.PerPage, detail.Events.Total)})
}

func (a *App) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	announcements := []store.Announcement{}
	attributes := []creator.ResourceAttribute{}
	if a.store != nil {
		page, err := a.store.AdminAnnouncementsPage(r.Context(), store.AdminAnnouncementQuery{Page: 1, PerPage: 5})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, item := range page.Items {
			announcements = append(announcements, item.Announcement)
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
	a.render(w, r, "admin_settings", map[string]any{
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
	position := 0
	if raw := strings.TrimSpace(r.FormValue("position")); raw != "" {
		position, err = strconv.Atoi(raw)
		if err != nil || position < 0 {
			http.Error(w, "invalid position", http.StatusBadRequest)
			return
		}
	}
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
	a.render(w, r, "admin_audit", map[string]any{"Title": "审计日志", "Logs": page.Items, "Page": page, "Query": page.Query, "From": r.URL.Query().Get("from"), "To": r.URL.Query().Get("to"), "ExportURL": exportURL, "Pager": web.NewPagination("/admin/audit", r.URL.Query(), page.Page, page.PerPage, page.Total)})
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
	a.render(w, r, "admin_audit_detail", map[string]any{"Title": "审计详情", "Item": item})
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
