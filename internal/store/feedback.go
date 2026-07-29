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

var ErrFeedbackNotFound = errors.New("feedback ticket not found")

const (
	FeedbackKindFeedback       = "feedback"
	FeedbackKindReports        = "reports"
	FeedbackKindResourceReport = "resource_report"
	FeedbackKindCommentReport  = "comment_report"
)

func IsReportKind(kind string) bool {
	return kind == FeedbackKindResourceReport ||
		kind == FeedbackKindCommentReport
}

type FeedbackTicket struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Subject      string          `json:"subject"`
	Message      string          `json:"message"`
	TargetSource string          `json:"target_source,omitempty"`
	TargetID     string          `json:"target_id,omitempty"`
	TargetURL    string          `json:"target_url,omitempty"`
	Status       string          `json:"status"`
	Resolution   string          `json:"resolution,omitempty"`
	UserID       string          `json:"user_id,omitempty"`
	Username     string          `json:"username,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	ClosedAt     *time.Time      `json:"closed_at,omitempty"`
	Replies      []FeedbackReply `json:"replies"`
}

type FeedbackReply struct {
	ID        string    `json:"id"`
	Author    string    `json:"author"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateFeedbackParams struct {
	UserID, Kind, Subject, Message, TargetSource, TargetID, TargetURL string
}

type AdminFeedbackQuery struct {
	Kind         string
	Status       string
	Search       string
	TargetSource string
	Page         int
	PerPage      int
}

type FeedbackPage struct {
	Items      []FeedbackTicket
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminFeedbackQuery
}

type FeedbackUpdate struct {
	Status     string
	Resolution *string
	Reply      string
	AuthorID   string
}

func validFeedbackStatus(status string) bool {
	switch status {
	case "open", "investigating", "replied", "resolved", "dismissed", "closed":
		return true
	default:
		return false
	}
}

func (s *Store) CreateFeedback(ctx context.Context, p CreateFeedbackParams) (FeedbackTicket, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO feedback_tickets(id,user_id,kind,subject,message,target_source,target_id,target_url) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, p.UserID, p.Kind, p.Subject, p.Message, p.TargetSource, p.TargetID, p.TargetURL)
	if err != nil {
		return FeedbackTicket{}, err
	}
	return s.Feedback(ctx, id, p.UserID, false)
}

func (s *Store) FeedbackTargetExists(ctx context.Context, source, id string) (bool, error) {
	source = strings.TrimSpace(source)
	id = strings.TrimSpace(id)
	if id == "" {
		return false, nil
	}
	if strings.EqualFold(source, "comment") {
		return s.CommentTargetExists(ctx, id)
	}
	if source != "" && !strings.EqualFold(source, "oronbox") {
		return true, nil
	}
	if _, err := uuid.Parse(id); err != nil {
		return false, nil
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resources WHERE id=$1)`, id).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *Store) Feedback(ctx context.Context, id, userID string, privileged bool) (FeedbackTicket, error) {
	var ticket FeedbackTicket
	if _, err := uuid.Parse(id); err != nil {
		return ticket, ErrFeedbackNotFound
	}
	query := `SELECT t.id::text,t.kind,t.subject,t.message,t.target_source,t.target_id,t.target_url,t.status,t.resolution,t.user_id::text,u.username,t.created_at,t.updated_at,t.closed_at FROM feedback_tickets t JOIN users u ON u.id=t.user_id WHERE t.id=$1`
	args := []any{id}
	if !privileged {
		query += ` AND t.user_id=$2`
		args = append(args, userID)
	}
	if err := scanFeedback(s.db.QueryRowContext(ctx, query, args...), &ticket); errors.Is(err, sql.ErrNoRows) {
		return ticket, ErrFeedbackNotFound
	} else if err != nil {
		return ticket, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT reply.id::text,COALESCE(NULLIF(author.username,''),'管理员'),reply.message,reply.created_at FROM feedback_replies reply LEFT JOIN users author ON author.id=reply.author_id WHERE reply.ticket_id=$1 ORDER BY reply.created_at`, id)
	if err != nil {
		return ticket, err
	}
	defer rows.Close()
	for rows.Next() {
		var reply FeedbackReply
		if err := rows.Scan(&reply.ID, &reply.Author, &reply.Message, &reply.CreatedAt); err != nil {
			return ticket, err
		}
		ticket.Replies = append(ticket.Replies, reply)
	}
	return ticket, rows.Err()
}

type feedbackScanner interface {
	Scan(dest ...any) error
}

func scanFeedback(scanner feedbackScanner, ticket *FeedbackTicket) error {
	var closedAt sql.NullTime
	if err := scanner.Scan(&ticket.ID, &ticket.Kind, &ticket.Subject, &ticket.Message, &ticket.TargetSource, &ticket.TargetID, &ticket.TargetURL, &ticket.Status, &ticket.Resolution, &ticket.UserID, &ticket.Username, &ticket.CreatedAt, &ticket.UpdatedAt, &closedAt); err != nil {
		return err
	}
	if closedAt.Valid {
		ticket.ClosedAt = &closedAt.Time
	}
	return nil
}

// FeedbackList preserves the user-facing list API. Administrative pages should
// use AdminFeedback so filters and pagination are applied in the database.
func (s *Store) FeedbackList(ctx context.Context, userID string, privileged bool, status string) ([]FeedbackTicket, error) {
	if privileged {
		page, err := s.AdminFeedback(ctx, AdminFeedbackQuery{Status: status, Page: 1, PerPage: 100})
		return page.Items, err
	}
	query := `SELECT t.id::text,t.kind,t.subject,t.message,t.target_source,t.target_id,t.target_url,t.status,t.resolution,t.user_id::text,u.username,t.created_at,t.updated_at,t.closed_at FROM feedback_tickets t JOIN users u ON u.id=t.user_id WHERE t.user_id=$1`
	args := []any{userID}
	if validFeedbackStatus(status) {
		query += ` AND t.status=$2`
		args = append(args, status)
	}
	query += ` ORDER BY t.updated_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tickets := []FeedbackTicket{}
	for rows.Next() {
		var ticket FeedbackTicket
		if err := scanFeedback(rows, &ticket); err != nil {
			return nil, err
		}
		tickets = append(tickets, ticket)
	}
	return tickets, rows.Err()
}

func (query AdminFeedbackQuery) normalized() AdminFeedbackQuery {
	query.Search = strings.TrimSpace(query.Search)
	query.TargetSource = strings.TrimSpace(query.TargetSource)
	if query.Kind != FeedbackKindFeedback && query.Kind != FeedbackKindReports && !IsReportKind(query.Kind) {
		query.Kind = ""
	}
	if !validFeedbackStatus(query.Status) {
		query.Status = ""
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = 25
	}
	if query.PerPage > 100 {
		query.PerPage = 100
	}
	return query
}

func (s *Store) AdminFeedback(ctx context.Context, raw AdminFeedbackQuery) (FeedbackPage, error) {
	query := raw.normalized()
	args := []any{}
	where := []string{"TRUE"}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if query.Kind != "" {
		if query.Kind == FeedbackKindReports {
			where = append(where, `ticket.kind IN ('resource_report','comment_report')`)
		} else {
			add(`ticket.kind=?`, query.Kind)
		}
	}
	if query.Status != "" {
		add(`ticket.status=?`, query.Status)
	}
	if query.TargetSource != "" {
		add(`ticket.target_source=?`, query.TargetSource)
	}
	if query.Search != "" {
		add(`(ticket.subject ILIKE '%'||?||'%' OR ticket.message ILIKE '%'||?||'%' OR ticket.target_id ILIKE '%'||?||'%' OR account.username ILIKE '%'||?||'%')`, query.Search)
	}
	base := `FROM feedback_tickets ticket JOIN users account ON account.id=ticket.user_id WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) `+base, args...).Scan(&total); err != nil {
		return FeedbackPage{}, err
	}
	args = append(args, query.PerPage, (query.Page-1)*query.PerPage)
	limitPosition, offsetPosition := len(args)-1, len(args)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT ticket.id::text,ticket.kind,ticket.subject,ticket.message,ticket.target_source,ticket.target_id,ticket.target_url,ticket.status,ticket.resolution,ticket.user_id::text,account.username,ticket.created_at,ticket.updated_at,ticket.closed_at
%s ORDER BY ticket.updated_at DESC,ticket.id DESC LIMIT $%d OFFSET $%d`, base, limitPosition, offsetPosition), args...)
	if err != nil {
		return FeedbackPage{}, err
	}
	defer rows.Close()
	page := FeedbackPage{Items: []FeedbackTicket{}, Page: query.Page, PerPage: query.PerPage, Query: query, Total: total}
	for rows.Next() {
		var ticket FeedbackTicket
		var closedAt sql.NullTime
		if err := rows.Scan(&ticket.ID, &ticket.Kind, &ticket.Subject, &ticket.Message, &ticket.TargetSource, &ticket.TargetID, &ticket.TargetURL, &ticket.Status, &ticket.Resolution, &ticket.UserID, &ticket.Username, &ticket.CreatedAt, &ticket.UpdatedAt, &closedAt); err != nil {
			return FeedbackPage{}, err
		}
		if closedAt.Valid {
			ticket.ClosedAt = &closedAt.Time
		}
		page.Items = append(page.Items, ticket)
	}
	if err := rows.Err(); err != nil {
		return FeedbackPage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

func (s *Store) UpdateFeedback(ctx context.Context, id string, update FeedbackUpdate) (FeedbackTicket, error) {
	if _, err := uuid.Parse(id); err != nil {
		return FeedbackTicket{}, ErrFeedbackNotFound
	}
	update.Status = strings.TrimSpace(update.Status)
	update.Reply = strings.TrimSpace(update.Reply)
	var resolution sql.NullString
	if update.Resolution != nil {
		resolution.Valid = true
		resolution.String = strings.TrimSpace(*update.Resolution)
	}
	if update.Status == "" && update.Reply != "" {
		update.Status = "replied"
	}
	if !validFeedbackStatus(update.Status) {
		return FeedbackTicket{}, fmt.Errorf("invalid feedback status %q", update.Status)
	}
	if len([]rune(update.Reply)) > 10000 || len([]rune(resolution.String)) > 4000 {
		return FeedbackTicket{}, fmt.Errorf("feedback reply or resolution is too long")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return FeedbackTicket{}, err
	}
	defer tx.Rollback()
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM feedback_tickets WHERE id=$1 FOR UPDATE`, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return FeedbackTicket{}, ErrFeedbackNotFound
	} else if err != nil {
		return FeedbackTicket{}, err
	}
	closed := update.Status == "resolved" || update.Status == "dismissed" || update.Status == "closed"
	if _, err := tx.ExecContext(ctx, `UPDATE feedback_tickets SET status=$2,resolution=CASE WHEN $3::text IS NULL THEN resolution ELSE $3 END,updated_at=now(),closed_at=CASE WHEN $4 THEN COALESCE(closed_at,now()) ELSE NULL END WHERE id=$1`, id, update.Status, resolution, closed); err != nil {
		return FeedbackTicket{}, err
	}
	if update.Reply != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO feedback_replies(id,ticket_id,author_id,message) VALUES($1,$2,NULLIF($3,'')::uuid,$4)`, uuid.NewString(), id, update.AuthorID, update.Reply); err != nil {
			return FeedbackTicket{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FeedbackTicket{}, err
	}
	return s.Feedback(ctx, id, "", true)
}

func (s *Store) ReplyFeedback(ctx context.Context, id, authorID, message string, closeTicket bool) (FeedbackTicket, error) {
	status := "replied"
	if closeTicket {
		status = "closed"
	}
	return s.UpdateFeedback(ctx, id, FeedbackUpdate{Status: status, Reply: message, AuthorID: authorID})
}
