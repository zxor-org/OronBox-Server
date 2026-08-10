package store

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/model"
)

const (
	adminDiagnosticDefaultPerPage = 25
	adminDiagnosticMaxPerPage     = 100
)

type AdminOAuthEventQuery struct {
	Search, App, Result, Platform string
	From, To                      *time.Time
	Page, PerPage                 int
}

type AdminOAuthEventPage struct {
	Items                            []model.OAuthEvent
	Total, Page, PerPage, TotalPages int
	Query                            AdminOAuthEventQuery
}

type AdminOAuthStateQuery struct {
	Search, App, Status, Platform string
	From, To                      *time.Time
	Page, PerPage                 int
}

type AdminOAuthStatePage struct {
	Items                            []model.OAuthState
	Total, Page, PerPage, TotalPages int
	Query                            AdminOAuthStateQuery
}

type AdminOAuthTicketQuery struct {
	Search, App, Status, Platform string
	From, To                      *time.Time
	Page, PerPage                 int
}

type AdminOAuthTicketPage struct {
	Items                            []model.OAuthTicket
	Total, Page, PerPage, TotalPages int
	Query                            AdminOAuthTicketQuery
}

type AdminClientStatsQuery struct {
	Search, App, Result, Platform string
	From, To                      *time.Time
	Page, PerPage                 int
}

var ErrAdminDiagnosticNotFound = errors.New("admin diagnostic record not found")

type AdminOAuthEventDetail struct {
	Event  model.OAuthEvent
	State  *model.OAuthState
	Ticket *model.OAuthTicket
}

type AdminOAuthStateDetail struct {
	State   model.OAuthState
	Events  []model.OAuthEvent
	Tickets []model.OAuthTicket
}

type AdminOAuthTicketDetail struct {
	Ticket model.OAuthTicket
	UserID string
	Events []model.OAuthEvent
	States []model.OAuthState
}

type AdminClientDetail struct {
	Stats  ClientStats
	Events AdminOAuthEventPage
}

type AdminClientStatsPage struct {
	Items                            []ClientStats
	Total, Page, PerPage, TotalPages int
	Query                            AdminClientStatsQuery
}

type AdminAuditLogQuery struct {
	Search, Result, TargetType, TargetID, ActorUserID string
	From, To                                          *time.Time
	Page, PerPage                                     int
}

type AdminAuditLogPage struct {
	Items                            []AuditLog
	Total, Page, PerPage, TotalPages int
	Query                            AdminAuditLogQuery
}

func normalizeAdminDiagnosticPage(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = adminDiagnosticDefaultPerPage
	}
	if perPage > adminDiagnosticMaxPerPage {
		perPage = adminDiagnosticMaxPerPage
	}
	return page, perPage
}

func normalizeAdminDiagnosticRange(from, to *time.Time) (*time.Time, *time.Time) {
	if from != nil && to != nil && from.After(*to) {
		return nil, nil
	}
	return from, to
}

func adminDiagnosticTotalPages(total, perPage int) int {
	if total == 0 || perPage < 1 {
		return 0
	}
	return (total + perPage - 1) / perPage
}

func normalizeDiagnosticStatus(value string) string {
	value = strings.TrimSpace(value)
	if value != "active" && value != "used" && value != "expired" {
		return ""
	}
	return value
}

func (q AdminOAuthEventQuery) normalized() AdminOAuthEventQuery {
	q.Search, q.App, q.Result, q.Platform = strings.TrimSpace(q.Search), strings.TrimSpace(q.App), normalizeDiagnosticResult(q.Result), strings.TrimSpace(q.Platform)
	q.From, q.To = normalizeAdminDiagnosticRange(q.From, q.To)
	q.Page, q.PerPage = normalizeAdminDiagnosticPage(q.Page, q.PerPage)
	return q
}

func (q AdminOAuthStateQuery) normalized() AdminOAuthStateQuery {
	q.Search, q.App, q.Platform, q.Status = strings.TrimSpace(q.Search), strings.TrimSpace(q.App), strings.TrimSpace(q.Platform), normalizeDiagnosticStatus(q.Status)
	q.From, q.To = normalizeAdminDiagnosticRange(q.From, q.To)
	q.Page, q.PerPage = normalizeAdminDiagnosticPage(q.Page, q.PerPage)
	return q
}

func (q AdminOAuthTicketQuery) normalized() AdminOAuthTicketQuery {
	q.Search, q.App, q.Platform, q.Status = strings.TrimSpace(q.Search), strings.TrimSpace(q.App), strings.TrimSpace(q.Platform), normalizeDiagnosticStatus(q.Status)
	q.From, q.To = normalizeAdminDiagnosticRange(q.From, q.To)
	q.Page, q.PerPage = normalizeAdminDiagnosticPage(q.Page, q.PerPage)
	return q
}

func (q AdminClientStatsQuery) normalized() AdminClientStatsQuery {
	q.Search, q.App, q.Result, q.Platform = strings.TrimSpace(q.Search), strings.TrimSpace(q.App), normalizeDiagnosticResult(q.Result), strings.TrimSpace(q.Platform)
	q.From, q.To = normalizeAdminDiagnosticRange(q.From, q.To)
	q.Page, q.PerPage = normalizeAdminDiagnosticPage(q.Page, q.PerPage)
	return q
}

func (q AdminAuditLogQuery) normalized() AdminAuditLogQuery {
	q.Search, q.Result = strings.TrimSpace(q.Search), strings.TrimSpace(q.Result)
	q.TargetType, q.TargetID, q.ActorUserID = strings.TrimSpace(q.TargetType), strings.TrimSpace(q.TargetID), strings.TrimSpace(q.ActorUserID)
	switch q.TargetType {
	case "", "resource", "user", "ticket", "feedback", "comment", "revision", "blob", "plugin", "collection", "publication":
	default:
		q.TargetType = ""
	}
	q.From, q.To = normalizeAdminDiagnosticRange(q.From, q.To)
	q.Page, q.PerPage = normalizeAdminDiagnosticPage(q.Page, q.PerPage)
	return q
}

func normalizeDiagnosticResult(value string) string {
	value = strings.TrimSpace(value)
	if value != "success" && value != "failure" {
		return ""
	}
	return value
}

func diagnosticArgs(search, app, status, platform string, from, to *time.Time) []any {
	return []any{search, app, status, platform, from, to}
}

func (s *Store) AdminOAuthEvents(ctx context.Context, raw AdminOAuthEventQuery) (AdminOAuthEventPage, error) {
	q := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',provider,event_type,result,app_id,app_version,app_build,platform,error_code,error_message,state_id,ticket_id,provider_user_id) ILIKE '%'||$1||'%') AND ($2='' OR app_id=$2) AND ($3='' OR result=$3) AND ($4='' OR platform=$4) AND ($5::timestamptz IS NULL OR created_at >= $5) AND ($6::timestamptz IS NULL OR created_at <= $6)`
	args := diagnosticArgs(q.Search, q.App, q.Result, q.Platform, q.From, q.To)
	page := AdminOAuthEventPage{Items: []model.OAuthEvent{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM oauth_events WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return AdminOAuthEventPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,created_at,provider,event_type,result,app_id,app_version,app_build,platform,COALESCE(ip::text,''),user_agent,state_id,ticket_id,provider_user_id,expected_scopes,actual_scopes,error_code,error_message,latency_ms FROM oauth_events WHERE `+filter+` ORDER BY id DESC LIMIT $7 OFFSET $8`, append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return AdminOAuthEventPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.OAuthEvent
		var created time.Time
		if err := rows.Scan(&item.ID, &created, &item.Provider, &item.EventType, &item.Result, &item.AppID, &item.AppVersion, &item.AppBuild, &item.Platform, &item.IP, &item.UserAgent, &item.StateID, &item.TicketID, &item.ProviderUserID, &item.ExpectedScopes, &item.ActualScopes, &item.ErrorCode, &item.ErrorMessage, &item.LatencyMS); err != nil {
			return AdminOAuthEventPage{}, err
		}
		item.CreatedAt = formatTime(created)
		sanitizeOAuthEvent(&item)
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminOAuthEventPage{}, err
	}
	page.TotalPages = adminDiagnosticTotalPages(page.Total, page.PerPage)
	return page, nil
}

func (s *Store) AdminOAuthStates(ctx context.Context, raw AdminOAuthStateQuery) (AdminOAuthStatePage, error) {
	q := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',id,app_id,app_version,app_build,platform,return_uri,provider,purpose,user_agent) ILIKE '%'||$1||'%') AND ($2='' OR app_id=$2) AND ($3='' OR ($3='active' AND used_at IS NULL AND expires_at>now()) OR ($3='used' AND used_at IS NOT NULL) OR ($3='expired' AND used_at IS NULL AND expires_at<=now())) AND ($4='' OR platform=$4) AND ($5::timestamptz IS NULL OR created_at >= $5) AND ($6::timestamptz IS NULL OR created_at <= $6)`
	args := diagnosticArgs(q.Search, q.App, q.Status, q.Platform, q.From, q.To)
	page := AdminOAuthStatePage{Items: []model.OAuthState{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM oauth_states WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return AdminOAuthStatePage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,created_at,expires_at,used_at,app_id,app_version,app_build,platform,return_uri,COALESCE(ip::text,''),user_agent,provider,purpose FROM oauth_states WHERE `+filter+` ORDER BY created_at DESC LIMIT $7 OFFSET $8`, append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return AdminOAuthStatePage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.OAuthState
		var created, expires time.Time
		var used sql.NullTime
		if err := rows.Scan(&item.ID, &created, &expires, &used, &item.AppID, &item.AppVersion, &item.AppBuild, &item.Platform, &item.ReturnURI, &item.IP, &item.UserAgent, &item.Provider, &item.Purpose); err != nil {
			return AdminOAuthStatePage{}, err
		}
		item.CreatedAt, item.ExpiresAt = formatTime(created), formatTime(expires)
		if used.Valid {
			item.UsedAt = formatTime(used.Time)
		}
		item.ReturnURI = redactDiagnosticText(item.ReturnURI)
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminOAuthStatePage{}, err
	}
	page.TotalPages = adminDiagnosticTotalPages(page.Total, page.PerPage)
	return page, nil
}

func (s *Store) AdminOAuthTickets(ctx context.Context, raw AdminOAuthTicketQuery) (AdminOAuthTicketPage, error) {
	q := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',t.id::text,t.app_id,t.platform,t.return_uri,u.username,u.bandbbs_user_id::text) ILIKE '%'||$1||'%') AND ($2='' OR t.app_id=$2) AND ($3='' OR ($3='active' AND t.used_at IS NULL AND t.expires_at>now()) OR ($3='used' AND t.used_at IS NOT NULL) OR ($3='expired' AND t.used_at IS NULL AND t.expires_at<=now())) AND ($4='' OR t.platform=$4) AND ($5::timestamptz IS NULL OR t.created_at >= $5) AND ($6::timestamptz IS NULL OR t.created_at <= $6)`
	args := diagnosticArgs(q.Search, q.App, q.Status, q.Platform, q.From, q.To)
	page := AdminOAuthTicketPage{Items: []model.OAuthTicket{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM login_tickets t JOIN users u ON u.id=t.user_id WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return AdminOAuthTicketPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id::text,t.created_at,t.expires_at,t.used_at,t.app_id,t.platform,t.return_uri,COALESCE(NULLIF(u.username,''),u.bandbbs_user_id::text,''),t.token_cipher IS NOT NULL FROM login_tickets t JOIN users u ON u.id=t.user_id WHERE `+filter+` ORDER BY t.created_at DESC LIMIT $7 OFFSET $8`, append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return AdminOAuthTicketPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.OAuthTicket
		var created, expires time.Time
		var used sql.NullTime
		if err := rows.Scan(&item.ID, &created, &expires, &used, &item.AppID, &item.Platform, &item.ReturnURI, &item.UserLabel, &item.HasToken); err != nil {
			return AdminOAuthTicketPage{}, err
		}
		item.CreatedAt, item.ExpiresAt = formatTime(created), formatTime(expires)
		if used.Valid {
			item.UsedAt = formatTime(used.Time)
		}
		item.ReturnURI = redactDiagnosticText(item.ReturnURI)
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminOAuthTicketPage{}, err
	}
	page.TotalPages = adminDiagnosticTotalPages(page.Total, page.PerPage)
	return page, nil
}

func (s *Store) AdminClientStats(ctx context.Context, raw AdminClientStatsQuery) (AdminClientStatsPage, error) {
	q := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',app_id,app_version,app_build,platform) ILIKE '%'||$1||'%') AND ($2='' OR app_id=$2) AND ($3='' OR result=$3) AND ($4='' OR platform=$4) AND ($5::timestamptz IS NULL OR created_at >= $5) AND ($6::timestamptz IS NULL OR created_at <= $6)`
	args := diagnosticArgs(q.Search, q.App, q.Result, q.Platform, q.From, q.To)
	page := AdminClientStatsPage{Items: []ClientStats{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT 1 FROM oauth_events WHERE `+filter+` GROUP BY app_id,app_version,app_build,platform) grouped`, args...).Scan(&page.Total); err != nil {
		return AdminClientStatsPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT app_id,app_version,app_build,platform,count(*),count(*) FILTER (WHERE result='success'),count(*) FILTER (WHERE result='failure'),max(created_at) FROM oauth_events WHERE `+filter+` GROUP BY app_id,app_version,app_build,platform ORDER BY max(created_at) DESC LIMIT $7 OFFSET $8`, append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return AdminClientStatsPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item ClientStats
		var last time.Time
		if err := rows.Scan(&item.AppID, &item.AppVersion, &item.AppBuild, &item.Platform, &item.RequestCount, &item.SuccessCount, &item.FailureCount, &last); err != nil {
			return AdminClientStatsPage{}, err
		}
		item.LastSeen = formatTime(last)
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminClientStatsPage{}, err
	}
	page.TotalPages = adminDiagnosticTotalPages(page.Total, page.PerPage)
	return page, nil
}

var diagnosticBearerPattern = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)
var diagnosticSecretPattern = regexp.MustCompile(`(?i)(access[_-]?token|refresh[_-]?token|id[_-]?token|client[_-]?secret|secret|token|ticket|code)(\s*[=:]\s*|%3[dD])([^&\s,;]+)`)

// redactDiagnosticText is deliberately applied in the Store so no admin view
// can accidentally render credentials copied into provider errors or URLs.
func redactDiagnosticText(value string) string {
	value = diagnosticBearerPattern.ReplaceAllString(value, `Bearer [REDACTED]`)
	return diagnosticSecretPattern.ReplaceAllString(value, `${1}${2}[REDACTED]`)
}

func sanitizeOAuthEvent(item *model.OAuthEvent) {
	item.ErrorMessage = redactDiagnosticText(item.ErrorMessage)
	item.ErrorCode = redactDiagnosticText(item.ErrorCode)
	item.UserAgent = redactDiagnosticText(item.UserAgent)
}

func (s *Store) AdminOAuthEvent(ctx context.Context, id int64) (AdminOAuthEventDetail, error) {
	var detail AdminOAuthEventDetail
	var created time.Time
	err := s.db.QueryRowContext(ctx, `SELECT id,created_at,provider,event_type,result,app_id,app_version,app_build,platform,COALESCE(ip::text,''),user_agent,state_id,ticket_id,provider_user_id,expected_scopes,actual_scopes,error_code,error_message,latency_ms FROM oauth_events WHERE id=$1`, id).Scan(&detail.Event.ID, &created, &detail.Event.Provider, &detail.Event.EventType, &detail.Event.Result, &detail.Event.AppID, &detail.Event.AppVersion, &detail.Event.AppBuild, &detail.Event.Platform, &detail.Event.IP, &detail.Event.UserAgent, &detail.Event.StateID, &detail.Event.TicketID, &detail.Event.ProviderUserID, &detail.Event.ExpectedScopes, &detail.Event.ActualScopes, &detail.Event.ErrorCode, &detail.Event.ErrorMessage, &detail.Event.LatencyMS)
	if errors.Is(err, sql.ErrNoRows) {
		return detail, ErrAdminDiagnosticNotFound
	}
	if err != nil {
		return detail, err
	}
	detail.Event.CreatedAt = formatTime(created)
	sanitizeOAuthEvent(&detail.Event)
	if detail.Event.StateID != "" {
		if state, stateErr := s.adminOAuthState(ctx, detail.Event.StateID); stateErr == nil {
			detail.State = &state
		} else if !errors.Is(stateErr, ErrAdminDiagnosticNotFound) {
			return detail, stateErr
		}
	}
	if detail.Event.TicketID != "" {
		if ticket, _, ticketErr := s.adminOAuthTicket(ctx, detail.Event.TicketID); ticketErr == nil {
			detail.Ticket = &ticket
		} else if !errors.Is(ticketErr, ErrAdminDiagnosticNotFound) {
			return detail, ticketErr
		}
	}
	return detail, nil
}

func (s *Store) adminOAuthState(ctx context.Context, id string) (model.OAuthState, error) {
	var item model.OAuthState
	var created, expires time.Time
	var used sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT id,created_at,expires_at,used_at,app_id,app_version,app_build,platform,return_uri,COALESCE(ip::text,''),user_agent,provider,purpose FROM oauth_states WHERE id=$1`, id).Scan(&item.ID, &created, &expires, &used, &item.AppID, &item.AppVersion, &item.AppBuild, &item.Platform, &item.ReturnURI, &item.IP, &item.UserAgent, &item.Provider, &item.Purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrAdminDiagnosticNotFound
	}
	if err != nil {
		return item, err
	}
	item.CreatedAt, item.ExpiresAt = formatTime(created), formatTime(expires)
	if used.Valid {
		item.UsedAt = formatTime(used.Time)
	}
	item.ReturnURI = redactDiagnosticText(item.ReturnURI)
	return item, nil
}

func (s *Store) AdminOAuthState(ctx context.Context, id string) (AdminOAuthStateDetail, error) {
	state, err := s.adminOAuthState(ctx, strings.TrimSpace(id))
	if err != nil {
		return AdminOAuthStateDetail{}, err
	}
	detail := AdminOAuthStateDetail{State: state, Events: []model.OAuthEvent{}, Tickets: []model.OAuthTicket{}}
	events, err := s.AdminOAuthEvents(ctx, AdminOAuthEventQuery{Search: state.ID, Page: 1, PerPage: 100})
	if err != nil {
		return detail, err
	}
	for _, event := range events.Items {
		if event.StateID != state.ID {
			continue
		}
		detail.Events = append(detail.Events, event)
		if event.TicketID == "" {
			continue
		}
		ticket, _, ticketErr := s.adminOAuthTicket(ctx, event.TicketID)
		if ticketErr == nil && !containsDiagnosticTicket(detail.Tickets, ticket.ID) {
			detail.Tickets = append(detail.Tickets, ticket)
		} else if ticketErr != nil && !errors.Is(ticketErr, ErrAdminDiagnosticNotFound) {
			return detail, ticketErr
		}
	}
	return detail, nil
}

func (s *Store) adminOAuthTicket(ctx context.Context, id string) (model.OAuthTicket, string, error) {
	var item model.OAuthTicket
	var userID string
	var created, expires time.Time
	var used sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT t.id::text,t.created_at,t.expires_at,t.used_at,t.app_id,t.platform,t.return_uri,COALESCE(NULLIF(u.username,''),u.bandbbs_user_id::text,''),t.token_cipher IS NOT NULL,t.user_id::text FROM login_tickets t JOIN users u ON u.id=t.user_id WHERE t.id::text=$1`, id).Scan(&item.ID, &created, &expires, &used, &item.AppID, &item.Platform, &item.ReturnURI, &item.UserLabel, &item.HasToken, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, "", ErrAdminDiagnosticNotFound
	}
	if err != nil {
		return item, "", err
	}
	item.CreatedAt, item.ExpiresAt = formatTime(created), formatTime(expires)
	if used.Valid {
		item.UsedAt = formatTime(used.Time)
	}
	item.ReturnURI = redactDiagnosticText(item.ReturnURI)
	return item, userID, nil
}

func (s *Store) AdminOAuthTicket(ctx context.Context, id string) (AdminOAuthTicketDetail, error) {
	ticket, userID, err := s.adminOAuthTicket(ctx, strings.TrimSpace(id))
	if err != nil {
		return AdminOAuthTicketDetail{}, err
	}
	detail := AdminOAuthTicketDetail{Ticket: ticket, UserID: userID, Events: []model.OAuthEvent{}, States: []model.OAuthState{}}
	events, err := s.AdminOAuthEvents(ctx, AdminOAuthEventQuery{Search: ticket.ID, Page: 1, PerPage: 100})
	if err != nil {
		return detail, err
	}
	for _, event := range events.Items {
		if event.TicketID != ticket.ID {
			continue
		}
		detail.Events = append(detail.Events, event)
		if event.StateID == "" {
			continue
		}
		state, stateErr := s.adminOAuthState(ctx, event.StateID)
		if stateErr == nil && !containsDiagnosticState(detail.States, state.ID) {
			detail.States = append(detail.States, state)
		} else if stateErr != nil && !errors.Is(stateErr, ErrAdminDiagnosticNotFound) {
			return detail, stateErr
		}
	}
	return detail, nil
}

func containsDiagnosticTicket(items []model.OAuthTicket, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func containsDiagnosticState(items []model.OAuthState, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func (s *Store) AdminClient(ctx context.Context, appID, version, build, platform string, raw AdminOAuthEventQuery) (AdminClientDetail, error) {
	appID, version, build, platform = strings.TrimSpace(appID), strings.TrimSpace(version), strings.TrimSpace(build), strings.TrimSpace(platform)
	var detail AdminClientDetail
	var last time.Time
	err := s.db.QueryRowContext(ctx, `SELECT app_id,app_version,app_build,platform,count(*),count(*) FILTER (WHERE result='success'),count(*) FILTER (WHERE result='failure'),max(created_at) FROM oauth_events WHERE app_id=$1 AND app_version=$2 AND app_build=$3 AND platform=$4 GROUP BY app_id,app_version,app_build,platform`, appID, version, build, platform).Scan(&detail.Stats.AppID, &detail.Stats.AppVersion, &detail.Stats.AppBuild, &detail.Stats.Platform, &detail.Stats.RequestCount, &detail.Stats.SuccessCount, &detail.Stats.FailureCount, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return detail, ErrAdminDiagnosticNotFound
	}
	if err != nil {
		return detail, err
	}
	detail.Stats.LastSeen = formatTime(last)
	q := raw.normalized()
	q.App, q.Platform = appID, platform
	page := AdminOAuthEventPage{Items: []model.OAuthEvent{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	args := []any{appID, version, build, platform, q.Result, q.From, q.To}
	const exactFilter = `app_id=$1 AND app_version=$2 AND app_build=$3 AND platform=$4 AND ($5='' OR result=$5) AND ($6::timestamptz IS NULL OR created_at >= $6) AND ($7::timestamptz IS NULL OR created_at <= $7)`
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM oauth_events WHERE `+exactFilter, args...).Scan(&page.Total); err != nil {
		return detail, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,created_at,provider,event_type,result,app_id,app_version,app_build,platform,COALESCE(ip::text,''),user_agent,state_id,ticket_id,provider_user_id,expected_scopes,actual_scopes,error_code,error_message,latency_ms FROM oauth_events WHERE `+exactFilter+` ORDER BY id DESC LIMIT $8 OFFSET $9`, append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return detail, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.OAuthEvent
		var created time.Time
		if err := rows.Scan(&item.ID, &created, &item.Provider, &item.EventType, &item.Result, &item.AppID, &item.AppVersion, &item.AppBuild, &item.Platform, &item.IP, &item.UserAgent, &item.StateID, &item.TicketID, &item.ProviderUserID, &item.ExpectedScopes, &item.ActualScopes, &item.ErrorCode, &item.ErrorMessage, &item.LatencyMS); err != nil {
			return detail, err
		}
		item.CreatedAt = formatTime(created)
		sanitizeOAuthEvent(&item)
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return detail, err
	}
	page.TotalPages = adminDiagnosticTotalPages(page.Total, page.PerPage)
	detail.Events = page
	return detail, nil
}

func (s *Store) AdminAuditLogs(ctx context.Context, raw AdminAuditLogQuery) (AdminAuditLogPage, error) {
	q := raw.normalized()
	args := adminAuditFilterArgs(q)
	page := AdminAuditLogPage{Items: []AuditLog{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs audit LEFT JOIN users actor ON actor.id=audit.actor_user_id WHERE `+adminAuditFilter, args...).Scan(&page.Total); err != nil {
		return AdminAuditLogPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, adminAuditSelect+` WHERE `+adminAuditFilter+` ORDER BY audit.id DESC LIMIT $8 OFFSET $9`, append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return AdminAuditLogPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return AdminAuditLogPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminAuditLogPage{}, err
	}
	page.TotalPages = adminDiagnosticTotalPages(page.Total, page.PerPage)
	return page, nil
}

const adminAuditFilter = `($1='' OR concat_ws(' ',audit.actor_user_id::text,actor.username,audit.action,audit.result,audit.ip::text,audit.user_agent,audit.metadata::text,audit.target_data::text,audit.before_data::text,audit.after_data::text) ILIKE '%'||$1||'%') AND ($2='' OR audit.result=$2) AND ($3::timestamptz IS NULL OR audit.created_at >= $3) AND ($4::timestamptz IS NULL OR audit.created_at <= $4) AND ($5='' OR audit.target_data->>'type'=$5 OR (audit.target_data='{}'::jsonb AND audit.metadata->>'message' ILIKE '%'||(CASE $5 WHEN 'resource' THEN 'resource=' WHEN 'user' THEN 'user=%' WHEN 'ticket' THEN 'ticket=' WHEN 'feedback' THEN 'ticket=' WHEN 'blob' THEN 'sha256=' WHEN 'comment' THEN 'comment=' WHEN 'revision' THEN 'revision=' WHEN 'plugin' THEN 'plugin=' WHEN 'collection' THEN 'collection=' WHEN 'publication' THEN 'publication=' ELSE '__no_legacy_target__' END)||'%')) AND ($6='' OR audit.target_data->>'id'=$6 OR (audit.target_data='{}'::jsonb AND audit.metadata->>'message' ILIKE '%'||$6||'%')) AND ($7='' OR audit.actor_user_id::text=$7)`

func adminAuditFilterArgs(q AdminAuditLogQuery) []any {
	return []any{q.Search, q.Result, q.From, q.To, q.TargetType, q.TargetID, q.ActorUserID}
}
