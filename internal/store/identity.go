package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/model"
)

var ErrInvalidCredential = errors.New("credential is invalid or expired")

// userColumns lists the users table columns userScanner expects, in order.
const userColumns = `id::text,bandbbs_user_id,username,avatar_url,role,banned_at,ban_reason,creator_frozen_at,created_at,updated_at`

// userColumnsAliased is userColumns with the "u" table alias used in joins.
const userColumnsAliased = `u.id::text,u.bandbbs_user_id,u.username,u.avatar_url,u.role,u.banned_at,u.ban_reason,u.creator_frozen_at,u.created_at,u.updated_at`

// userScanner scans a row whose user columns follow userColumns order,
// optionally wrapped by prefix/suffix destinations.
type userScanner struct {
	user                      *model.User
	bannedAt, creatorFrozenAt sql.NullTime
}

func newUserScanner(user *model.User) *userScanner {
	return &userScanner{user: user}
}

func (scanner *userScanner) dest(prefix, suffix []any) []any {
	user := scanner.user
	return append(append(append([]any{}, prefix...),
		&user.ID, &user.BandBBSUserID, &user.Username, &user.AvatarURL, &user.Role,
		&scanner.bannedAt, &user.BanReason, &scanner.creatorFrozenAt, &user.CreatedAt, &user.UpdatedAt), suffix...)
}

func (scanner *userScanner) finish() {
	if scanner.bannedAt.Valid {
		scanner.user.BannedAt = &scanner.bannedAt.Time
	}
	if scanner.creatorFrozenAt.Valid {
		scanner.user.CreatorFrozenAt = &scanner.creatorFrozenAt.Time
	}
}

type UpsertUserParams struct {
	BandBBSUserID int64
	Username      string
	AvatarURL     string
}

type GrantParams struct {
	UserID             string
	Provider           string
	Subject            string
	Scopes             []string
	AccessTokenCipher  []byte
	RefreshTokenCipher []byte
	TokenType          string
	ExpiresAt          *time.Time
}

type OAuthGrant struct {
	PendingCipher      []byte
	Invalid            bool
	Subject            string
	Scopes             []string
	AccessTokenCipher  []byte
	RefreshTokenCipher []byte
	TokenType          string
	ExpiresAt          *time.Time
}

func (s *Store) OAuthGrant(ctx context.Context, userID, provider string) (OAuthGrant, error) {
	var grant OAuthGrant
	var expiry sql.NullTime
	var scopesJSON []byte
	err := s.db.QueryRowContext(ctx, `SELECT subject,to_json(scopes),access_token_cipher,COALESCE(refresh_token_cipher,''::bytea),token_type,expires_at,COALESCE(pending_token_cipher,''::bytea),invalid_at IS NOT NULL FROM oauth_grants WHERE user_id=$1 AND provider=$2`, userID, provider).Scan(&grant.Subject, &scopesJSON, &grant.AccessTokenCipher, &grant.RefreshTokenCipher, &grant.TokenType, &expiry, &grant.PendingCipher, &grant.Invalid)
	if err != nil {
		return grant, err
	}
	if err := json.Unmarshal(scopesJSON, &grant.Scopes); err != nil {
		return grant, fmt.Errorf("decode OAuth grant scopes: %w", err)
	}
	if expiry.Valid {
		grant.ExpiresAt = &expiry.Time
	}
	return grant, nil
}

type LoginTicketParams struct {
	Purpose     string
	TicketHash  []byte
	UserID      string
	ExpiresAt   time.Time
	Meta        model.ClientMeta
	ReturnURI   string
	TokenCipher []byte
}

type SessionParams struct {
	RotationRequestHash    []byte
	RotationResponseCipher []byte
	AccessHash             []byte
	RefreshHash            []byte
	AccessExpiresAt        time.Time
	RefreshExpiresAt       time.Time
	Meta                   model.ClientMeta
}

func (s *Store) UpsertUser(ctx context.Context, params UpsertUserParams) (model.User, error) {
	var user model.User
	scanner := newUserScanner(&user)
	err := s.db.QueryRowContext(ctx, `
INSERT INTO users(id, bandbbs_user_id, username, avatar_url)
VALUES($1,$2,$3,$4)
ON CONFLICT(bandbbs_user_id) DO UPDATE SET username=excluded.username, avatar_url=excluded.avatar_url, updated_at=now()
RETURNING `+userColumns,
		uuid.NewString(), params.BandBBSUserID, params.Username, params.AvatarURL,
	).Scan(scanner.dest(nil, nil)...)
	if err != nil {
		return user, err
	}
	scanner.finish()
	return user, nil
}

func (s *Store) SetUserRole(ctx context.Context, userID, role string) (model.User, error) {
	var user model.User
	scanner := newUserScanner(&user)
	err := s.db.QueryRowContext(ctx, `UPDATE users SET role=$2,updated_at=now() WHERE id=$1 RETURNING `+userColumns, userID, role).
		Scan(scanner.dest(nil, nil)...)
	if err != nil {
		return user, err
	}
	scanner.finish()
	return user, nil
}

func (s *Store) UserByID(ctx context.Context, userID string) (model.User, error) {
	var user model.User
	scanner := newUserScanner(&user)
	err := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, userID).
		Scan(scanner.dest(nil, nil)...)
	if err != nil {
		return user, err
	}
	scanner.finish()
	return user, nil
}

func (s *Store) UpsertOAuthGrant(ctx context.Context, params GrantParams) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_grants(id,user_id,provider,subject,scopes,access_token_cipher,refresh_token_cipher,token_type,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(user_id,provider) DO UPDATE SET subject=excluded.subject, scopes=excluded.scopes,
 access_token_cipher=excluded.access_token_cipher, refresh_token_cipher=excluded.refresh_token_cipher,
 token_type=excluded.token_type, expires_at=excluded.expires_at, pending_token_cipher=NULL,invalid_at=NULL,updated_at=now()`,
		uuid.NewString(), params.UserID, params.Provider, params.Subject, params.Scopes,
		params.AccessTokenCipher, nullableBytes(params.RefreshTokenCipher), params.TokenType, params.ExpiresAt)
	return err
}

func (s *Store) CreateLoginTicket(ctx context.Context, params LoginTicketParams) (string, error) {
	id := uuid.NewString()
	if params.Purpose == "" {
		params.Purpose = "client_login"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO login_tickets(id,ticket_hash,user_id,expires_at,app_id,platform,return_uri,token_cipher,purpose)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, params.TicketHash, params.UserID, params.ExpiresAt,
		params.Meta.AppID, params.Meta.Platform, params.ReturnURI, nullableBytes(params.TokenCipher), params.Purpose)
	return id, err
}

func (s *Store) ConsumeLoginTicket(ctx context.Context, ticketHash []byte, session SessionParams) (model.User, []byte, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return model.User{}, nil, err
	}
	defer tx.Rollback()
	var user model.User
	scanner := newUserScanner(&user)
	var ticketID string
	var tokenCipher []byte
	err = tx.QueryRowContext(ctx, `
SELECT t.id::text,`+userColumnsAliased+`,COALESCE(t.token_cipher,''::bytea)
FROM login_tickets t JOIN users u ON u.id=t.user_id
WHERE t.ticket_hash=$1 AND t.purpose IN('client_login','client_publish') AND t.app_id=$2 AND t.platform=$3 AND t.used_at IS NULL AND t.expires_at>now() AND u.banned_at IS NULL FOR UPDATE`, ticketHash, session.Meta.AppID, session.Meta.Platform).
		Scan(scanner.dest([]any{&ticketID}, []any{&tokenCipher})...)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, nil, ErrInvalidCredential
	}
	if err != nil {
		return model.User{}, nil, err
	}
	scanner.finish()
	if _, err = tx.ExecContext(ctx, `UPDATE login_tickets SET used_at=now() WHERE id=$1`, ticketID); err != nil {
		return model.User{}, nil, err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO sessions(id,user_id,access_hash,refresh_hash,access_expires_at,refresh_expires_at,app_id,app_version,platform,ip,user_agent)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,'')::inet,$11)`, uuid.NewString(), user.ID,
		session.AccessHash, session.RefreshHash, session.AccessExpiresAt, session.RefreshExpiresAt,
		session.Meta.AppID, session.Meta.Version, session.Meta.Platform, session.Meta.IP, session.Meta.UA)
	if err != nil {
		return model.User{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return model.User{}, nil, err
	}
	return user, tokenCipher, nil
}

func (s *Store) UserByAccessToken(ctx context.Context, accessHash []byte) (model.User, error) {
	var user model.User
	scanner := newUserScanner(&user)
	err := s.db.QueryRowContext(ctx, `
SELECT `+userColumnsAliased+`
FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.access_hash=$1 AND s.revoked_at IS NULL AND s.access_expires_at>now()`, accessHash).
		Scan(scanner.dest(nil, nil)...)
	if errors.Is(err, sql.ErrNoRows) {
		return model.User{}, ErrInvalidCredential
	}
	if err != nil {
		return user, err
	}
	scanner.finish()
	return user, nil
}

func (s *Store) RevokeSession(ctx context.Context, accessHash []byte) error {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE access_hash=$1 AND revoked_at IS NULL`, accessHash)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrInvalidCredential
	}
	return nil
}

func (s *Store) RotateSession(ctx context.Context, hash []byte, session SessionParams) (model.User, error) {
	user, _, err := s.RotateSessionWithReceipt(ctx, hash, session)
	return user, err
}

func (s *Store) RotateSessionWithReceipt(ctx context.Context, currentRefreshHash []byte, session SessionParams) (model.User, []byte, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, nil, err
	}
	defer tx.Rollback()
	var sessionID string
	var user model.User
	scanner := newUserScanner(&user)
	err = tx.QueryRowContext(ctx, `
SELECT s.id::text,`+userColumnsAliased+`
FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.refresh_hash=$1 AND s.revoked_at IS NULL AND s.refresh_expires_at>now() AND u.banned_at IS NULL FOR UPDATE`, currentRefreshHash).
		Scan(scanner.dest([]any{&sessionID}, nil)...)
	if errors.Is(err, sql.ErrNoRows) {
		if len(session.RotationRequestHash) > 0 {
			var cipher []byte
			err = tx.QueryRowContext(ctx, `SELECT `+userColumnsAliased+`,r.response_cipher FROM session_rotation_receipts r JOIN sessions s ON s.id=r.session_id AND s.refresh_hash=r.current_refresh_hash JOIN users u ON u.id=s.user_id WHERE r.refresh_hash=$1 AND r.request_hash=$2 AND r.expires_at>now() AND s.revoked_at IS NULL AND u.banned_at IS NULL`, currentRefreshHash, session.RotationRequestHash).Scan(scanner.dest(nil, []any{&cipher})...)
			if err == nil {
				scanner.finish()
				return user, cipher, tx.Commit()
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return model.User{}, nil, err
			}
		}
		return model.User{}, nil, ErrInvalidCredential
	}
	if err != nil {
		return model.User{}, nil, err
	}
	scanner.finish()
	_, err = tx.ExecContext(ctx, `
UPDATE sessions SET access_hash=$1,refresh_hash=$2,access_expires_at=$3,refresh_expires_at=$4,last_seen_at=now(),
 app_id=$5,app_version=$6,platform=$7,ip=NULLIF($8,'')::inet,user_agent=$9 WHERE id=$10`,
		session.AccessHash, session.RefreshHash, session.AccessExpiresAt, session.RefreshExpiresAt,
		session.Meta.AppID, session.Meta.Version, session.Meta.Platform, session.Meta.IP, session.Meta.UA, sessionID)
	if err != nil {
		return model.User{}, nil, err
	}
	if len(session.RotationRequestHash) > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO session_rotation_receipts(refresh_hash,request_hash,session_id,current_refresh_hash,response_cipher,expires_at)VALUES($1,$2,$3,$4,$5,now()+interval '2 minutes')`, currentRefreshHash, session.RotationRequestHash, sessionID, session.RefreshHash, session.RotationResponseCipher)
		if err != nil {
			return model.User{}, nil, err
		}
	}
	return user, nil, tx.Commit()
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func (s *Store) WithOAuthGrantLock(ctx context.Context, userID, provider string, work func(*Store) error) error {
	db := s.db.(*sql.DB)
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	key := userID + ":" + provider
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1,9742))`, key); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return err
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(release, `SELECT pg_advisory_unlock(hashtextextended($1,9742))`, key); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	return work(&Store{db: conn})
}

func (s *Store) SavePendingOAuthToken(ctx context.Context, userID, provider string, cipher []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE oauth_grants SET pending_token_cipher=$3,updated_at=now() WHERE user_id=$1 AND provider=$2`, userID, provider, cipher)
	return err
}

func (s *Store) InvalidateOAuthGrant(ctx context.Context, userID, provider string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE oauth_grants SET invalid_at=now(),pending_token_cipher=NULL WHERE user_id=$1 AND provider=$2`, userID, provider)
	return err
}

// Revocation accepts an expired AT or an RT and is idempotent. It never grants
// access and permits logout without first creating another token pair.
func (s *Store) RevokeSessionCredentials(ctx context.Context, accessHash, refreshHash []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id::text FROM sessions WHERE (access_hash=$1 OR refresh_hash=$2) AND revoked_at IS NULL FOR UPDATE`, accessHash, refreshHash).Scan(&id)
	if err == sql.ErrNoRows {
		// A concurrent rotation may have replaced both hashes while we waited.
		err = tx.QueryRowContext(ctx, `SELECT s.id::text FROM sessions s JOIN session_rotation_receipts r ON r.session_id=s.id WHERE r.refresh_hash=$1 AND r.expires_at>now() AND s.revoked_at IS NULL FOR UPDATE OF s`, refreshHash).Scan(&id)
	}
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}
