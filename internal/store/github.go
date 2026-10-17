package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

func (s *Store) GitHubAccessTokenCipher(ctx context.Context, userID string) ([]byte, error) {
	var cipher []byte
	err := s.db.QueryRowContext(ctx, `SELECT access_token_cipher FROM github_grants WHERE user_id=$1`, userID).Scan(&cipher)
	return cipher, err
}

func (s *Store) GitHubGrantScopes(ctx context.Context, userID string) ([]string, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT to_json(scopes) FROM github_grants WHERE user_id=$1`, userID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var scopes []string
	if err := json.Unmarshal(raw, &scopes); err != nil {
		return nil, err
	}
	return scopes, err
}

func (s *Store) GitHubLogin(ctx context.Context, userID string) (string, error) {
	var login string
	err := s.db.QueryRowContext(ctx, `SELECT login FROM github_grants WHERE user_id=$1`, userID).Scan(&login)
	return login, err
}

type GitHubDeviceFlow struct {
	ID               string
	DeviceCodeCipher []byte
	Interval         int
	ExpiresAt        time.Time
	NextPollAt       time.Time
}

func (s *Store) CreateGitHubDeviceFlow(ctx context.Context, userID string, cipher []byte, userCode, verificationURI string, interval int, expiresAt time.Time) (string, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO github_device_flows(id,user_id,device_code_cipher,user_code,verification_uri,interval_seconds,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, userID, cipher, userCode, verificationURI, interval, expiresAt)
	return id, err
}
func (s *Store) GitHubDeviceFlow(ctx context.Context, id, userID string) (GitHubDeviceFlow, error) {
	var flow GitHubDeviceFlow
	err := s.db.QueryRowContext(ctx, `SELECT id::text,device_code_cipher,interval_seconds,expires_at,next_poll_at FROM github_device_flows WHERE id=$1 AND user_id=$2 AND completed_at IS NULL AND expires_at>now()`, id, userID).Scan(&flow.ID, &flow.DeviceCodeCipher, &flow.Interval, &flow.ExpiresAt, &flow.NextPollAt)
	if errors.Is(err, sql.ErrNoRows) {
		return flow, ErrInvalidCredential
	}
	return flow, err
}
func (s *Store) CompleteGitHubDeviceFlow(ctx context.Context, flowID, userID string, githubID int64, login string, tokenCipher []byte, scopes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE github_device_flows SET completed_at=now() WHERE id=$1 AND user_id=$2 AND completed_at IS NULL AND expires_at>now() AND EXISTS(SELECT 1 FROM users WHERE id=$2 AND banned_at IS NULL)`, flowID, userID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrInvalidCredential
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO github_grants(user_id,github_user_id,login,access_token_cipher,scopes) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id) DO UPDATE SET github_user_id=excluded.github_user_id,login=excluded.login,access_token_cipher=excluded.access_token_cipher,scopes=excluded.scopes,updated_at=now()`, userID, githubID, login, tokenCipher, scopes)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CompleteGitHubWebFlow(ctx context.Context, flowID, userID string, githubID int64, login string, tokenCipher []byte, scopes []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO github_grants(user_id,github_user_id,login,access_token_cipher,scopes) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id) DO UPDATE SET github_user_id=excluded.github_user_id,login=excluded.login,access_token_cipher=excluded.access_token_cipher,scopes=excluded.scopes,updated_at=now()`, userID, githubID, login, tokenCipher, scopes); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE oauth_states SET completed_at=now() WHERE id=$1 AND provider='github' AND user_id=$2 AND used_at IS NOT NULL AND completed_at IS NULL AND expires_at>now() AND EXISTS(SELECT 1 FROM users WHERE id=$2 AND banned_at IS NULL)`, flowID, userID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrInvalidCredential
	}
	return tx.Commit()
}

func (s *Store) GitHubWebFlowStatus(ctx context.Context, flowID, userID string) (string, bool, error) {
	var login string
	err := s.db.QueryRowContext(ctx, `SELECT g.login FROM oauth_states s JOIN github_grants g ON g.user_id=s.user_id WHERE s.id=$1 AND s.provider='github' AND s.user_id=$2 AND s.completed_at IS NOT NULL`, flowID, userID).Scan(&login)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return login, err == nil, err
}

func (s *Store) DeleteGitHubGrant(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{`DELETE FROM github_grants WHERE user_id=$1`, `DELETE FROM oauth_states WHERE user_id=$1 AND provider='github'`, `DELETE FROM github_device_flows WHERE user_id=$1`} {
		if _, err = tx.ExecContext(ctx, q, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RejectGitHubToken(ctx context.Context, userID string, cipher []byte) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM github_grants WHERE user_id=$1 AND access_token_cipher=$2`, userID, cipher)
	return err
}
func (s *Store) DelayGitHubDevicePoll(ctx context.Context, id string, slow bool) (int, error) {
	var interval int
	err := s.db.QueryRowContext(ctx, `UPDATE github_device_flows SET interval_seconds=GREATEST(interval_seconds,5)+CASE WHEN $2 THEN 5 ELSE 0 END,next_poll_at=now()+make_interval(secs=>GREATEST(interval_seconds,5)+CASE WHEN $2 THEN 5 ELSE 0 END) WHERE id=$1 RETURNING interval_seconds`, id, slow).Scan(&interval)
	return interval, err
}

func (s *Store) ClaimGitHubDevicePoll(ctx context.Context, id, userID string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE github_device_flows SET next_poll_at=now()+make_interval(secs=>GREATEST(interval_seconds,5)) WHERE id=$1 AND user_id=$2 AND next_poll_at<=now() AND completed_at IS NULL AND expires_at>now()`, id, userID)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}
