package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zxor-org/OronBox-Server/internal/model"
)

type CreateStateParams struct {
	ID           string
	Provider     string
	ExpiresAt    time.Time
	Meta         model.ClientMeta
	ReturnURI    string
	Purpose      string
	UserID       string
	SecretCipher []byte
}

type StateRecord struct {
	ID           string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	UsedAt       sql.NullTime
	AppID        string
	AppVersion   string
	AppBuild     string
	Platform     string
	ReturnURI    string
	IP           string
	UserAgent    string
	Purpose      string
	UserID       string
	SecretCipher []byte
}

func (s *Store) CreateState(ctx context.Context, params CreateStateParams) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_states
(id, provider, purpose, expires_at, app_id, app_version, app_build, platform, return_uri, user_id, ip, user_agent, secret_cipher)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10,'')::uuid, NULLIF($11,'')::inet, $12, $13)`,
		params.ID,
		params.Provider,
		params.Purpose,
		params.ExpiresAt.UTC(),
		params.Meta.AppID,
		params.Meta.Version,
		params.Meta.Build,
		params.Meta.Platform,
		params.ReturnURI,
		params.UserID,
		params.Meta.IP,
		params.Meta.UA,
		params.SecretCipher,
	)
	return err
}

func (s *Store) ConsumeState(ctx context.Context, provider, id string) (StateRecord, error) {
	var rec StateRecord
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return rec, err
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
SELECT id, created_at, expires_at, used_at, app_id, app_version, app_build, platform, return_uri,
COALESCE(user_id::text,''),ip,user_agent,purpose,secret_cipher
FROM oauth_states
WHERE id = $1 AND provider = $2 FOR UPDATE`, id, provider)
	if err := row.Scan(&rec.ID, &rec.CreatedAt, &rec.ExpiresAt, &rec.UsedAt, &rec.AppID, &rec.AppVersion, &rec.AppBuild, &rec.Platform, &rec.ReturnURI, &rec.UserID, &rec.IP, &rec.UserAgent, &rec.Purpose, &rec.SecretCipher); err != nil {
		return rec, err
	}
	if rec.UsedAt.Valid {
		return rec, errors.New("state already used")
	}
	if time.Now().UTC().After(rec.ExpiresAt) {
		return rec, errors.New("state expired")
	}
	result, err := tx.ExecContext(ctx, `UPDATE oauth_states SET used_at=now() WHERE id=$1 AND provider=$2 AND used_at IS NULL`, id, provider)
	if err != nil {
		return rec, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return rec, errors.New("state already consumed")
	}
	return rec, tx.Commit()
}

func (s *Store) RecordOAuthEvent(ctx context.Context, event model.OAuthEvent) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_events
(created_at, provider, event_type, result, app_id, app_version, app_build, platform, ip, user_agent, state_id, ticket_id, provider_user_id, expected_scopes, actual_scopes, error_code, error_message, latency_ms)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		time.Now().UTC(),
		event.Provider,
		event.EventType,
		event.Result,
		event.AppID,
		event.AppVersion,
		event.AppBuild,
		event.Platform,
		event.IP,
		event.UserAgent,
		event.StateID,
		event.TicketID,
		event.ProviderUserID,
		event.ExpectedScopes,
		event.ActualScopes,
		event.ErrorCode,
		event.ErrorMessage,
		event.LatencyMS,
	)
	return err
}

func (s *Store) CleanupExpired(ctx context.Context) (int64, int64, error) {
	stateResult, err := s.db.ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at < now()`)
	if err != nil {
		return 0, 0, err
	}
	ticketResult, err := s.db.ExecContext(ctx, `DELETE FROM login_tickets WHERE expires_at < now()`)
	if err != nil {
		return 0, 0, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM admin_sessions WHERE expires_at < now()`); err != nil {
		return 0, 0, err
	}
	stateRows, _ := stateResult.RowsAffected()
	ticketRows, _ := ticketResult.RowsAffected()
	return stateRows, ticketRows, nil
}

type GrantSummary struct {
	Providers      []string `json:"providers"`
	GitHubLogin    string   `json:"github_login,omitempty"`
	BandBBSPublish bool     `json:"bandbbs_publish"`
}

func (s *Store) Grants(ctx context.Context, userID string) (GrantSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT provider FROM oauth_grants WHERE user_id=$1 ORDER BY provider`, userID)
	if err != nil {
		return GrantSummary{}, err
	}
	defer rows.Close()
	summary := GrantSummary{}
	for rows.Next() {
		var provider string
		if err := rows.Scan(&provider); err != nil {
			return GrantSummary{}, err
		}
		summary.Providers = append(summary.Providers, provider)
	}
	if err := rows.Err(); err != nil {
		return GrantSummary{}, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT 'resource:write'=ANY(scopes) FROM oauth_grants WHERE user_id=$1 AND provider='bandbbs_publish'`, userID).Scan(&summary.BandBBSPublish); err == nil {
	} else if !errors.Is(err, sql.ErrNoRows) {
		return GrantSummary{}, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT login FROM github_grants WHERE user_id=$1`, userID).Scan(&summary.GitHubLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return summary, nil
	}
	if err != nil {
		return GrantSummary{}, err
	}
	summary.Providers = append(summary.Providers, "github")
	return summary, nil
}
