package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

const AdminIdleTTL = 12 * time.Hour
const AdminAbsoluteTTL = 7 * 24 * time.Hour
const AdminOverlapTTL = 30 * time.Second

func AdminCredentialHash(raw string) string {
	v := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(v[:])
}
func (s *Store) CreateAdminSession(ctx context.Context, credential, userID, username, ip, ua string, expiresAt time.Time) error {
	hash := AdminCredentialHash(credential)
	_, err := s.db.ExecContext(ctx, `INSERT INTO admin_sessions(id,created_at,expires_at,absolute_expires_at,credential_hash,user_id,username,ip,user_agent)VALUES($1,now(),LEAST($2,now()+interval '12 hours'),now()+interval '7 days',$1,$3,$4,NULLIF($5,'')::inet,$6)`, hash, expiresAt, userID, username, ip, ua)
	return err
}
func (s *Store) AdminSession(ctx context.Context, credential string) (AdminSession, error) {
	var out AdminSession
	err := s.db.QueryRowContext(ctx, `SELECT id,user_id::text,username,expires_at,absolute_expires_at,generation,COALESCE(rotated_at,created_at) FROM admin_sessions WHERE (credential_hash=$1 OR (previous_hash=$1 AND previous_until>now())) AND expires_at>now() AND absolute_expires_at>now()`, AdminCredentialHash(credential)).Scan(&out.ID, &out.UserID, &out.Username, &out.ExpiresAt, &out.AbsoluteExpiresAt, &out.Generation, &out.RotatedAt)
	return out, err
}
func (s *Store) RotateAdminSession(ctx context.Context, id, presentedHash, nextHash string, generation int64) (AdminSession, error) {
	var out AdminSession
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var current string
	var previous sql.NullString
	var previousUntil, rotated sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,user_id::text,username,expires_at,absolute_expires_at,generation,credential_hash,previous_hash,previous_until,rotated_at FROM admin_sessions WHERE id=$1 AND expires_at>now() AND absolute_expires_at>now() FOR UPDATE`, id).Scan(&out.ID, &out.UserID, &out.Username, &out.ExpiresAt, &out.AbsoluteExpiresAt, &out.Generation, &current, &previous, &previousUntil, &rotated)
	if err != nil {
		return out, err
	}
	if rotated.Valid {
		out.RotatedAt = rotated.Time
	}
	now := time.Now().UTC()
	if current != presentedHash && !(previous.Valid && previous.String == presentedHash && previousUntil.Valid && previousUntil.Time.After(now)) {
		return out, sql.ErrNoRows
	}
	if out.Generation != generation || (rotated.Valid && rotated.Time.Add(AdminOverlapTTL).After(now)) {
		return out, tx.Commit()
	}
	if current != presentedHash {
		return out, sql.ErrNoRows
	}
	err = tx.QueryRowContext(ctx, `UPDATE admin_sessions SET previous_hash=credential_hash,previous_until=now()+interval '30 seconds',credential_hash=$2,generation=generation+1,rotated_at=now(),expires_at=LEAST(now()+interval '12 hours',absolute_expires_at) WHERE id=$1 RETURNING expires_at,generation,rotated_at`, id, nextHash).Scan(&out.ExpiresAt, &out.Generation, &out.RotatedAt)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) TouchAdminSession(ctx context.Context, id string) (AdminSession, error) {
	var out AdminSession
	err := s.db.QueryRowContext(ctx, `UPDATE admin_sessions SET expires_at=LEAST(now()+interval '12 hours',absolute_expires_at) WHERE id=$1 AND expires_at>now() AND absolute_expires_at>now() RETURNING id,user_id::text,username,expires_at,absolute_expires_at,generation,COALESCE(rotated_at,created_at)`, id).Scan(&out.ID, &out.UserID, &out.Username, &out.ExpiresAt, &out.AbsoluteExpiresAt, &out.Generation, &out.RotatedAt)
	return out, err
}
