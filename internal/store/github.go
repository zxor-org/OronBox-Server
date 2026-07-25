package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

type GitHubDeviceFlow struct {
	ID               string
	DeviceCodeCipher []byte
	Interval         int
	ExpiresAt        time.Time
}

func (s *Store) CreateGitHubDeviceFlow(ctx context.Context, userID string, cipher []byte, userCode, verificationURI string, interval int, expiresAt time.Time) (string, error) {
	id := uuid.NewString()
	_, err := s.db.ExecContext(ctx, `INSERT INTO github_device_flows(id,user_id,device_code_cipher,user_code,verification_uri,interval_seconds,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, userID, cipher, userCode, verificationURI, interval, expiresAt)
	return id, err
}
func (s *Store) GitHubDeviceFlow(ctx context.Context, id, userID string) (GitHubDeviceFlow, error) {
	var flow GitHubDeviceFlow
	err := s.db.QueryRowContext(ctx, `SELECT id::text,device_code_cipher,interval_seconds,expires_at FROM github_device_flows WHERE id=$1 AND user_id=$2 AND completed_at IS NULL AND expires_at>now()`, id, userID).Scan(&flow.ID, &flow.DeviceCodeCipher, &flow.Interval, &flow.ExpiresAt)
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
	result, err := tx.ExecContext(ctx, `UPDATE github_device_flows SET completed_at=now() WHERE id=$1 AND user_id=$2 AND completed_at IS NULL`, flowID, userID)
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
	result, err := tx.ExecContext(ctx, `UPDATE oauth_states SET completed_at=now() WHERE id=$1 AND provider='github' AND user_id=$2 AND used_at IS NOT NULL AND completed_at IS NULL`, flowID, userID)
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
	_, err := s.db.ExecContext(ctx, `DELETE FROM github_grants WHERE user_id=$1`, userID)
	return err
}
