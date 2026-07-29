package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/model"
)

type AdminSession struct {
	ID        string
	UserID    string
	Username  string
	ExpiresAt time.Time
}

type AuditLog struct {
	ID          int64
	CreatedAt   string
	ActorUserID string
	Username    string
	Action      string
	Result      string
	IP          string
	UserAgent   string
	Message     string
}

type ClientStats struct {
	AppID        string
	AppVersion   string
	AppBuild     string
	Platform     string
	RequestCount int64
	SuccessCount int64
	FailureCount int64
	LastSeen     string
}

func (s *Store) CreateAdminSession(ctx context.Context, id, userID, username, ip, ua string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO admin_sessions (id, created_at, expires_at, user_id, username, ip, user_agent)
VALUES ($1, now(), $2, $3, $4, NULLIF($5,'')::inet, $6)`, id, expiresAt, userID, username, ip, ua)
	return err
}

func (s *Store) AdminSession(ctx context.Context, id string) (AdminSession, error) {
	var session AdminSession
	row := s.db.QueryRowContext(ctx, `SELECT id, user_id::text, username, expires_at FROM admin_sessions WHERE id = $1`, id)
	if err := row.Scan(&session.ID, &session.UserID, &session.Username, &session.ExpiresAt); err != nil {
		return session, err
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE id=$1`, id)
		return session, sql.ErrNoRows
	}
	return session, nil
}

func (s *Store) DeleteAdminSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE id = $1`, id)
	return err
}

func (s *Store) RecordAudit(ctx context.Context, actor AdminSession, action, result, ip, ua, message string) error {
	metadata, err := json.Marshal(map[string]string{"username": actor.Username, "message": message})
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,action,result,ip,user_agent,metadata)
VALUES(NULLIF($1,'')::uuid,$2,$3,NULLIF($4,'')::inet,$5,$6)`, actor.UserID, action, result, ip, ua, metadata)
	return err
}

func (s *Store) Stats(ctx context.Context, startedAt time.Time) (model.Stats, error) {
	stats := model.Stats{StartedAt: startedAt}
	queries := []struct {
		dest      *int64
		eventType string
		result    string
	}{
		{&stats.OAuthStartToday, "start", ""},
		{&stats.CallbackOKToday, "callback", "success"},
		{&stats.CallbackFailToday, "callback", "failure"},
		{&stats.ExchangeOKToday, "ticket_exchange", "success"},
		{&stats.RefreshOKToday, "refresh", "success"},
		{&stats.RefreshFailToday, "refresh", "failure"},
		{&stats.ScopeMismatchToday, "scope_check", "failure"},
	}
	for _, query := range queries {
		sqlText := `SELECT COUNT(*) FROM oauth_events WHERE created_at >= date_trunc('day', now()) AND event_type = $1`
		args := []any{query.eventType}
		if query.result != "" {
			sqlText += ` AND result = $2`
			args = append(args, query.result)
		}
		if err := s.db.QueryRowContext(ctx, sqlText, args...).Scan(query.dest); err != nil {
			return stats, err
		}
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_states WHERE expires_at > now() AND used_at IS NULL`).Scan(&stats.ActiveStates); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM login_tickets WHERE expires_at > now() AND used_at IS NULL`).Scan(&stats.ActiveTickets); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER (WHERE moderation_state='visible' AND current_revision_id IS NOT NULL) FROM resources`).Scan(&stats.ResourcesTotal, &stats.PublishedResources); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM review_cases WHERE state='pending'`).Scan(&stats.PendingReviews); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM feedback_tickets WHERE kind IN ('resource_report','comment_report') AND status IN ('open','investigating','replied')`).Scan(&stats.OpenReports); err != nil {
		return stats, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM publications WHERE state='failed'`).Scan(&stats.FailedPublications); err != nil {
		return stats, err
	}
	return stats, nil
}

func (s *Store) RecentEvents(ctx context.Context, limit int) ([]model.OAuthEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, created_at, provider, event_type, result, app_id, app_version, app_build, platform,
 COALESCE(ip::text,''), user_agent, state_id, ticket_id, provider_user_id, expected_scopes, actual_scopes,
 error_code, error_message, latency_ms
FROM oauth_events ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := make([]model.OAuthEvent, 0)
	for rows.Next() {
		var event model.OAuthEvent
		var createdAt time.Time
		if err := rows.Scan(&event.ID, &createdAt, &event.Provider, &event.EventType, &event.Result, &event.AppID, &event.AppVersion, &event.AppBuild, &event.Platform, &event.IP, &event.UserAgent, &event.StateID, &event.TicketID, &event.ProviderUserID, &event.ExpectedScopes, &event.ActualScopes, &event.ErrorCode, &event.ErrorMessage, &event.LatencyMS); err != nil {
			return nil, err
		}
		event.CreatedAt = formatTime(createdAt)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) ActiveStates(ctx context.Context, limit int) ([]model.OAuthState, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, created_at, expires_at, used_at, app_id, app_version, app_build, platform, return_uri,
 COALESCE(ip::text,''), user_agent, provider, purpose
FROM oauth_states ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := make([]model.OAuthState, 0)
	for rows.Next() {
		var state model.OAuthState
		var createdAt, expiresAt time.Time
		var usedAt sql.NullTime
		if err := rows.Scan(&state.ID, &createdAt, &expiresAt, &usedAt, &state.AppID, &state.AppVersion, &state.AppBuild, &state.Platform, &state.ReturnURI, &state.IP, &state.UserAgent, &state.Provider, &state.Purpose); err != nil {
			return nil, err
		}
		state.CreatedAt = formatTime(createdAt)
		state.ExpiresAt = formatTime(expiresAt)
		if usedAt.Valid {
			state.UsedAt = formatTime(usedAt.Time)
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (s *Store) Tickets(ctx context.Context, limit int) ([]model.OAuthTicket, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT t.id::text, t.created_at, t.expires_at, t.used_at, t.app_id, t.platform, t.return_uri,
 COALESCE(NULLIF(u.username,''), u.bandbbs_user_id::text, ''), t.token_cipher IS NOT NULL
FROM login_tickets t JOIN users u ON u.id=t.user_id ORDER BY t.created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tickets := make([]model.OAuthTicket, 0)
	for rows.Next() {
		var ticket model.OAuthTicket
		var createdAt, expiresAt time.Time
		var usedAt sql.NullTime
		if err := rows.Scan(&ticket.ID, &createdAt, &expiresAt, &usedAt, &ticket.AppID, &ticket.Platform, &ticket.ReturnURI, &ticket.UserLabel, &ticket.HasToken); err != nil {
			return nil, err
		}
		ticket.CreatedAt = formatTime(createdAt)
		ticket.ExpiresAt = formatTime(expiresAt)
		if usedAt.Valid {
			ticket.UsedAt = formatTime(usedAt.Time)
		}
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}

func (s *Store) ClientStats(ctx context.Context, limit int) ([]ClientStats, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT app_id, app_version, app_build, platform, COUNT(*),
 COUNT(*) FILTER (WHERE result='success'), COUNT(*) FILTER (WHERE result='failure'), MAX(created_at)
FROM oauth_events GROUP BY app_id, app_version, app_build, platform ORDER BY MAX(created_at) DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := make([]ClientStats, 0)
	for rows.Next() {
		var stat ClientStats
		var lastSeen time.Time
		if err := rows.Scan(&stat.AppID, &stat.AppVersion, &stat.AppBuild, &stat.Platform, &stat.RequestCount, &stat.SuccessCount, &stat.FailureCount, &lastSeen); err != nil {
			return nil, err
		}
		stat.LastSeen = formatTime(lastSeen)
		stats = append(stats, stat)
	}
	return stats, rows.Err()
}

func (s *Store) AuditLogs(ctx context.Context, limit int) ([]AuditLog, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT audit.id,audit.created_at,COALESCE(audit.actor_user_id::text,''),
 COALESCE(NULLIF(actor.username,''),audit.metadata->>'username',''),audit.action,audit.result,
 COALESCE(audit.ip::text,''),audit.user_agent,COALESCE(audit.metadata->>'message','')
FROM audit_logs audit
LEFT JOIN users actor ON actor.id=audit.actor_user_id
ORDER BY audit.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]AuditLog, 0)
	for rows.Next() {
		var log AuditLog
		var createdAt time.Time
		if err := rows.Scan(&log.ID, &createdAt, &log.ActorUserID, &log.Username, &log.Action, &log.Result, &log.IP, &log.UserAgent, &log.Message); err != nil {
			return nil, err
		}
		log.CreatedAt = formatTime(createdAt)
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

func formatTime(value time.Time) string {
	return value.Local().Format("2006-01-02 15:04:05")
}
