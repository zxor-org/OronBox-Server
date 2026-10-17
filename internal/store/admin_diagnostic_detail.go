package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/model"
)

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
