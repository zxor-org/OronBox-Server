package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrAdminMessageNotFound = errors.New("system message was not found")

type AdminMessageQuery struct {
	Search, Kind, Read string
	User               string
	From, To           *time.Time
	Page, PerPage      int
}

type AdminMessage struct {
	ID, UserID, Username, Kind, Title, Body, Ref string
	ReadAt                                       *time.Time
	CreatedAt, ExpiresAt                         time.Time
}

type AdminMessagePage struct {
	Items                            []AdminMessage
	Total, Page, PerPage, TotalPages int
	Query                            AdminMessageQuery
}

const (
	adminMessageTitleSQL = `COALESCE(NULLIF(m.title,''),NULLIF(m.data->>'title',''),NULLIF(m.event,''),'')`
	adminMessageBodySQL  = `COALESCE(NULLIF(m.body,''),NULLIF(m.data->>'body',''),'')`
	adminMessageRefSQL   = `COALESCE(NULLIF(m.ref,''),NULLIF(m.data->>'ref',''),'')`
)

func (query AdminMessageQuery) normalized() AdminMessageQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.User = strings.TrimSpace(query.User)
	switch query.Kind {
	case "review_result", "moderation", "comment_reply", "report_result", "admin_message", "account", "announcement":
	default:
		query.Kind = ""
	}
	if query.Read != "read" && query.Read != "unread" {
		query.Read = ""
	}
	query.Page, query.PerPage = normalizeAdminDiagnosticPage(query.Page, query.PerPage)
	query.From, query.To = normalizeAdminDiagnosticRange(query.From, query.To)
	return query
}

func adminMessageFilter(query AdminMessageQuery) (string, []any) {
	args := []any{}
	where := []string{"TRUE"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.Search != "" {
		add(`concat_ws(' ',m.id::text,`+adminMessageTitleSQL+`,`+adminMessageBodySQL+`,`+adminMessageRefSQL+`,m.kind,m.event,m.data::text,u.id::text,u.username) ILIKE '%'||?||'%'`, query.Search)
	}
	if query.Kind != "" {
		add(`m.kind=?`, query.Kind)
	}
	if query.Read == "read" {
		where = append(where, "m.read_at IS NOT NULL")
	} else if query.Read == "unread" {
		where = append(where, "m.read_at IS NULL")
	}
	if query.User != "" {
		add(`(u.id::text=? OR u.username ILIKE '%'||?||'%')`, query.User)
	}
	if query.From != nil {
		add(`m.created_at>=?`, *query.From)
	}
	if query.To != nil {
		add(`m.created_at<=?`, *query.To)
	}
	return strings.Join(where, " AND "), args
}

func (s *Store) AdminMessages(ctx context.Context, raw AdminMessageQuery) (AdminMessagePage, error) {
	query := raw.normalized()
	where, args := adminMessageFilter(query)
	base := ` FROM user_messages m JOIN users u ON u.id=m.user_id WHERE ` + where
	page := AdminMessagePage{Items: []AdminMessage{}, Page: query.Page, PerPage: query.PerPage, Query: query}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*)`+base, args...).Scan(&page.Total); err != nil {
		return AdminMessagePage{}, err
	}
	args = append(args, query.PerPage, (query.Page-1)*query.PerPage)
	rows, err := s.db.QueryContext(ctx, `SELECT m.id::text,m.user_id::text,u.username,m.kind,`+adminMessageTitleSQL+`,`+adminMessageBodySQL+`,`+adminMessageRefSQL+`,m.read_at,m.created_at,m.expires_at`+base+fmt.Sprintf(` ORDER BY m.created_at DESC,m.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return AdminMessagePage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanAdminMessage(rows)
		if err != nil {
			return AdminMessagePage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminMessagePage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

func (s *Store) AdminMessage(ctx context.Context, id string) (AdminMessage, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminMessage{}, ErrAdminMessageNotFound
	}
	item, err := scanAdminMessage(s.db.QueryRowContext(ctx, `SELECT m.id::text,m.user_id::text,u.username,m.kind,`+adminMessageTitleSQL+`,`+adminMessageBodySQL+`,`+adminMessageRefSQL+`,m.read_at,m.created_at,m.expires_at FROM user_messages m JOIN users u ON u.id=m.user_id WHERE m.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminMessage{}, ErrAdminMessageNotFound
	}
	return item, err
}

type adminMessageScanner interface{ Scan(...any) error }

func scanAdminMessage(scanner adminMessageScanner) (AdminMessage, error) {
	var item AdminMessage
	var readAt sql.NullTime
	err := scanner.Scan(&item.ID, &item.UserID, &item.Username, &item.Kind, &item.Title, &item.Body, &item.Ref, &readAt, &item.CreatedAt, &item.ExpiresAt)
	if readAt.Valid {
		item.ReadAt = &readAt.Time
	}
	return item, err
}
