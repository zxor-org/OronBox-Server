package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	adminUserDetailDefaultPerPage = 25
	adminUserDetailMaxPerPage     = 100
)

// AdminUserDetailPageQuery allows every workspace group to paginate
// independently. A zero value means the first 25 records.
type AdminUserDetailPageQuery struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
}

func (q AdminUserDetailPageQuery) normalized() AdminUserDetailPageQuery {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 {
		q.PerPage = adminUserDetailDefaultPerPage
	}
	if q.PerPage > adminUserDetailMaxPerPage {
		q.PerPage = adminUserDetailMaxPerPage
	}
	return q
}

type AdminUserDetailQuery struct {
	Resources AdminUserDetailPageQuery `json:"resources"`
	Comments  AdminUserDetailPageQuery `json:"comments"`
	Tickets   AdminUserDetailPageQuery `json:"tickets"`
	Messages  AdminUserDetailPageQuery `json:"messages"`
	Ledger    AdminUserDetailPageQuery `json:"ledger"`
	Sessions  AdminUserDetailPageQuery `json:"sessions"`
	Audit     AdminUserDetailPageQuery `json:"audit"`
}

func (q AdminUserDetailQuery) normalized() AdminUserDetailQuery {
	q.Resources = q.Resources.normalized()
	q.Comments = q.Comments.normalized()
	q.Tickets = q.Tickets.normalized()
	q.Messages = q.Messages.normalized()
	q.Ledger = q.Ledger.normalized()
	q.Sessions = q.Sessions.normalized()
	q.Audit = q.Audit.normalized()
	return q
}

type AdminUserDetailPage[T any] struct {
	Items      []T `json:"items"`
	Total      int `json:"total"`
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
}

func newAdminUserDetailPage[T any](q AdminUserDetailPageQuery) AdminUserDetailPage[T] {
	return AdminUserDetailPage[T]{Items: []T{}, Page: q.Page, PerPage: q.PerPage}
}

func finishAdminUserDetailPage[T any](page *AdminUserDetailPage[T]) {
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
}

type AdminUserResource struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	Kind            string    `json:"kind"`
	Platform        string    `json:"platform"`
	ModerationState string    `json:"moderation_state"`
	RevisionState   string    `json:"revision_state"`
	DownloadCount   int       `json:"download_count"`
	RevisionNo      int       `json:"revision_no"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type AdminUserComment struct {
	ID              string     `json:"id"`
	ResourceID      string     `json:"resource_id"`
	ResourceName    string     `json:"resource_name"`
	Body            string     `json:"body"`
	ModerationState string     `json:"moderation_state"`
	ParentID        string     `json:"parent_id,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	EditedAt        *time.Time `json:"edited_at,omitempty"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
}

type AdminUserTicket struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Subject      string     `json:"subject"`
	Message      string     `json:"message"`
	TargetSource string     `json:"target_source"`
	TargetID     string     `json:"target_id"`
	TargetURL    string     `json:"target_url"`
	Status       string     `json:"status"`
	Resolution   string     `json:"resolution"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ClosedAt     *time.Time `json:"closed_at,omitempty"`
}

type AdminUserMessage struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Ref       string     `json:"ref"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt time.Time  `json:"expires_at"`
}

type AdminUserCoinLedgerEntry struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	ReferenceType string    `json:"reference_type"`
	ReferenceID   string    `json:"reference_id"`
	Note          string    `json:"note"`
	ActorUserID   string    `json:"actor_user_id"`
	DeltaUnits    int64     `json:"delta_units"`
	CreatedAt     time.Time `json:"created_at"`
}

type AdminUserSession struct {
	ID               string    `json:"id"`
	AppID            string    `json:"app_id"`
	AppVersion       string    `json:"app_version"`
	Platform         string    `json:"platform"`
	IP               string    `json:"ip"`
	UserAgent        string    `json:"user_agent"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	CreatedAt        time.Time `json:"created_at"`
	LastSeenAt       time.Time `json:"last_seen_at"`
}

type AdminUserAuditEntry struct {
	ID        int64     `json:"id"`
	Action    string    `json:"action"`
	Result    string    `json:"result"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Metadata  string    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
}

type AdminUserDetail struct {
	User      AdminUserItem                                 `json:"user"`
	Resources AdminUserDetailPage[AdminUserResource]        `json:"resources"`
	Comments  AdminUserDetailPage[AdminUserComment]         `json:"comments"`
	Tickets   AdminUserDetailPage[AdminUserTicket]          `json:"tickets"`
	Messages  AdminUserDetailPage[AdminUserMessage]         `json:"messages"`
	Coin      CoinAccount                                   `json:"coin"`
	Ledger    AdminUserDetailPage[AdminUserCoinLedgerEntry] `json:"ledger"`
	Sessions  AdminUserDetailPage[AdminUserSession]         `json:"sessions"`
	Audit     AdminUserDetailPage[AdminUserAuditEntry]      `json:"audit"`
	Query     AdminUserDetailQuery                          `json:"query"`
}

func (s *Store) AdminUserDetail(ctx context.Context, id string, raw AdminUserDetailQuery) (AdminUserDetail, error) {
	if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
		return AdminUserDetail{}, ErrAdminUserNotFound
	}
	q := raw.normalized()
	detail := AdminUserDetail{Query: q}
	var err error
	detail.User, err = scanAdminUser(s.db.QueryRowContext(ctx, `SELECT `+adminUserSelect+` FROM users u WHERE u.id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUserDetail{}, ErrAdminUserNotFound
	}
	if err != nil {
		return AdminUserDetail{}, err
	}
	if detail.Resources, err = s.adminUserResources(ctx, id, q.Resources); err != nil {
		return AdminUserDetail{}, err
	}
	if detail.Comments, err = s.adminUserComments(ctx, id, q.Comments); err != nil {
		return AdminUserDetail{}, err
	}
	if detail.Tickets, err = s.adminUserTickets(ctx, id, q.Tickets); err != nil {
		return AdminUserDetail{}, err
	}
	if detail.Messages, err = s.adminUserMessages(ctx, id, q.Messages); err != nil {
		return AdminUserDetail{}, err
	}
	var units int64
	var coinFrozenAt sql.NullTime
	err = s.db.QueryRowContext(ctx, `SELECT balance_units,voting_frozen_at,voting_frozen_reason FROM user_coin_accounts WHERE user_id=$1`, id).
		Scan(&units, &coinFrozenAt, &detail.Coin.VotingFrozenReason)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return AdminUserDetail{}, err
	}
	detail.Coin = coinAccount(units, coinFrozenAt, detail.Coin.VotingFrozenReason)
	if detail.Ledger, err = s.adminUserLedger(ctx, id, q.Ledger); err != nil {
		return AdminUserDetail{}, err
	}
	if detail.Sessions, err = s.adminUserSessions(ctx, id, q.Sessions); err != nil {
		return AdminUserDetail{}, err
	}
	if detail.Audit, err = s.adminUserAudit(ctx, id, q.Audit); err != nil {
		return AdminUserDetail{}, err
	}
	return detail, nil
}

func pageOffset(q AdminUserDetailPageQuery) int { return (q.Page - 1) * q.PerPage }

func (s *Store) adminUserResources(ctx context.Context, id string, q AdminUserDetailPageQuery) (AdminUserDetailPage[AdminUserResource], error) {
	p := newAdminUserDetailPage[AdminUserResource](q)
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM resources WHERE owner_id=$1`, id).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.id::text,r.slug,COALESCE(NULLIF(rr.name,''),r.draft_name),r.kind,r.platform,r.moderation_state,COALESCE(rr.state,''),COALESCE(rr.revision_no,0),r.download_count,r.created_at,r.updated_at FROM resources r LEFT JOIN resource_revisions rr ON rr.id=r.current_revision_id WHERE r.owner_id=$1 ORDER BY r.updated_at DESC,r.id DESC LIMIT $2 OFFSET $3`, id, q.PerPage, pageOffset(q))
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AdminUserResource
		if err := rows.Scan(&v.ID, &v.Slug, &v.Name, &v.Kind, &v.Platform, &v.ModerationState, &v.RevisionState, &v.RevisionNo, &v.DownloadCount, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return p, err
		}
		p.Items = append(p.Items, v)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	finishAdminUserDetailPage(&p)
	return p, nil
}

func (s *Store) adminUserComments(ctx context.Context, id string, q AdminUserDetailPageQuery) (AdminUserDetailPage[AdminUserComment], error) {
	p := newAdminUserDetailPage[AdminUserComment](q)
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_comments WHERE user_id=$1`, id).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id::text,c.resource_id::text,COALESCE(rr.name,r.draft_name),COALESCE(c.parent_id::text,''),c.body,c.moderation_state,c.created_at,c.edited_at,c.deleted_at FROM resource_comments c JOIN resources r ON r.id=c.resource_id LEFT JOIN resource_revisions rr ON rr.id=r.current_revision_id WHERE c.user_id=$1 ORDER BY c.created_at DESC,c.id DESC LIMIT $2 OFFSET $3`, id, q.PerPage, pageOffset(q))
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AdminUserComment
		var edited, deleted sql.NullTime
		if err := rows.Scan(&v.ID, &v.ResourceID, &v.ResourceName, &v.ParentID, &v.Body, &v.ModerationState, &v.CreatedAt, &edited, &deleted); err != nil {
			return p, err
		}
		if edited.Valid {
			v.EditedAt = &edited.Time
		}
		if deleted.Valid {
			v.DeletedAt = &deleted.Time
		}
		p.Items = append(p.Items, v)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	finishAdminUserDetailPage(&p)
	return p, nil
}

func (s *Store) adminUserTickets(ctx context.Context, id string, q AdminUserDetailPageQuery) (AdminUserDetailPage[AdminUserTicket], error) {
	p := newAdminUserDetailPage[AdminUserTicket](q)
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM feedback_tickets WHERE user_id=$1`, id).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,kind,subject,message,target_source,target_id,target_url,status,resolution,created_at,updated_at,closed_at FROM feedback_tickets WHERE user_id=$1 ORDER BY updated_at DESC,id DESC LIMIT $2 OFFSET $3`, id, q.PerPage, pageOffset(q))
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AdminUserTicket
		var closed sql.NullTime
		if err := rows.Scan(&v.ID, &v.Kind, &v.Subject, &v.Message, &v.TargetSource, &v.TargetID, &v.TargetURL, &v.Status, &v.Resolution, &v.CreatedAt, &v.UpdatedAt, &closed); err != nil {
			return p, err
		}
		if closed.Valid {
			v.ClosedAt = &closed.Time
		}
		p.Items = append(p.Items, v)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	finishAdminUserDetailPage(&p)
	return p, nil
}

func (s *Store) adminUserMessages(ctx context.Context, id string, q AdminUserDetailPageQuery) (AdminUserDetailPage[AdminUserMessage], error) {
	p := newAdminUserDetailPage[AdminUserMessage](q)
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1`, id).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.id::text,m.kind,`+adminMessageTitleSQL+`,`+adminMessageBodySQL+`,`+adminMessageRefSQL+`,m.read_at,m.created_at,m.expires_at FROM user_messages m WHERE m.user_id=$1 ORDER BY m.created_at DESC,m.id DESC LIMIT $2 OFFSET $3`, id, q.PerPage, pageOffset(q))
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AdminUserMessage
		var read sql.NullTime
		if err := rows.Scan(&v.ID, &v.Kind, &v.Title, &v.Body, &v.Ref, &read, &v.CreatedAt, &v.ExpiresAt); err != nil {
			return p, err
		}
		if read.Valid {
			v.ReadAt = &read.Time
		}
		p.Items = append(p.Items, v)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	finishAdminUserDetailPage(&p)
	return p, nil
}

func (s *Store) adminUserLedger(ctx context.Context, id string, q AdminUserDetailPageQuery) (AdminUserDetailPage[AdminUserCoinLedgerEntry], error) {
	p := newAdminUserDetailPage[AdminUserCoinLedgerEntry](q)
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM coin_ledger WHERE user_id=$1`, id).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,delta_units,kind,reference_type,reference_id,note,COALESCE(actor_id::text,''),created_at FROM coin_ledger WHERE user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, id, q.PerPage, pageOffset(q))
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AdminUserCoinLedgerEntry
		if err := rows.Scan(&v.ID, &v.DeltaUnits, &v.Kind, &v.ReferenceType, &v.ReferenceID, &v.Note, &v.ActorUserID, &v.CreatedAt); err != nil {
			return p, err
		}
		p.Items = append(p.Items, v)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	finishAdminUserDetailPage(&p)
	return p, nil
}

func (s *Store) adminUserSessions(ctx context.Context, id string, q AdminUserDetailPageQuery) (AdminUserDetailPage[AdminUserSession], error) {
	p := newAdminUserDetailPage[AdminUserSession](q)
	const active = `user_id=$1 AND revoked_at IS NULL AND refresh_expires_at>now()`
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE `+active, id).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,app_id,app_version,platform,COALESCE(ip::text,''),user_agent,access_expires_at,refresh_expires_at,created_at,last_seen_at FROM sessions WHERE `+active+` ORDER BY last_seen_at DESC,id DESC LIMIT $2 OFFSET $3`, id, q.PerPage, pageOffset(q))
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AdminUserSession
		if err := rows.Scan(&v.ID, &v.AppID, &v.AppVersion, &v.Platform, &v.IP, &v.UserAgent, &v.AccessExpiresAt, &v.RefreshExpiresAt, &v.CreatedAt, &v.LastSeenAt); err != nil {
			return p, err
		}
		p.Items = append(p.Items, v)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	finishAdminUserDetailPage(&p)
	return p, nil
}

func (s *Store) adminUserAudit(ctx context.Context, id string, q AdminUserDetailPageQuery) (AdminUserDetailPage[AdminUserAuditEntry], error) {
	p := newAdminUserDetailPage[AdminUserAuditEntry](q)
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs WHERE actor_user_id=$1`, id).Scan(&p.Total); err != nil {
		return p, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,action,result,COALESCE(ip::text,''),user_agent,metadata::text,created_at FROM audit_logs WHERE actor_user_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, id, q.PerPage, pageOffset(q))
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AdminUserAuditEntry
		if err := rows.Scan(&v.ID, &v.Action, &v.Result, &v.IP, &v.UserAgent, &v.Metadata, &v.CreatedAt); err != nil {
			return p, err
		}
		p.Items = append(p.Items, v)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	finishAdminUserDetailPage(&p)
	return p, nil
}

// AdminRevokeUserSession revokes one active client session owned by the user.
// Passing identifiers in the wrong user/session pairing is intentionally
// reported as not found so callers cannot mutate another user's session.
func (s *Store) AdminRevokeUserSession(ctx context.Context, userID, sessionID string) error {
	if _, err := uuid.Parse(userID); err != nil {
		return ErrAdminUserNotFound
	}
	if _, err := uuid.Parse(sessionID); err != nil {
		return ErrAdminUserSessionNotFound
	}
	return s.adminRevokeUserSessions(ctx, userID, sessionID)
}

// AdminRevokeAllUserSessions atomically revokes every active client session
// for an existing user and returns the number changed.
func (s *Store) AdminRevokeAllUserSessions(ctx context.Context, userID string) (int64, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return 0, ErrAdminUserNotFound
	}
	return s.adminRevokeAllUserSessions(ctx, userID)
}

var ErrAdminUserSessionNotFound = errors.New("active user session was not found")

func (s *Store) adminRevokeUserSessions(ctx context.Context, userID, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT true FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return ErrAdminUserNotFound
	}
	if err != nil {
		return err
	}
	var lockedID string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM sessions WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL AND refresh_expires_at>now() FOR UPDATE`, sessionID, userID).Scan(&lockedID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAdminUserSessionNotFound
	}
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, sessionID, userID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ErrAdminUserSessionNotFound
	}
	return tx.Commit()
}

func (s *Store) adminRevokeAllUserSessions(ctx context.Context, userID string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT true FROM users WHERE id=$1 FOR UPDATE`, userID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrAdminUserNotFound
	}
	if err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM sessions WHERE user_id=$1 AND revoked_at IS NULL AND refresh_expires_at>now() FOR UPDATE`, userID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL AND refresh_expires_at>now()`, userID)
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}
