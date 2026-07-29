package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type AdminCoinStats struct {
	IssuedUnits   int64 `json:"issued_units"`
	SpentUnits    int64 `json:"spent_units"`
	RewardedUnits int64 `json:"rewarded_units"`
	ActiveVoters  int64 `json:"active_voters"`
	FrozenVoters  int64 `json:"frozen_voters"`
}

type AdminCoinEntry struct {
	ID            string `json:"id"`
	UserID        string `json:"user_id"`
	Username      string `json:"username"`
	DeltaUnits    int64  `json:"delta_units"`
	Kind          string `json:"kind"`
	ReferenceType string `json:"reference_type"`
	ReferenceID   string `json:"reference_id"`
	Note          string `json:"note"`
	CreatedAt     string `json:"created_at"`
}

func (s *Store) AdminCoinStats(ctx context.Context) (AdminCoinStats, error) {
	var result AdminCoinStats
	err := s.db.QueryRowContext(ctx, `SELECT
COALESCE(sum(delta_units) FILTER (WHERE kind IN ('checkin','admin_adjustment') AND delta_units>0),0),
COALESCE(-sum(delta_units) FILTER (WHERE kind='resource_vote'),0),
COALESCE(sum(delta_units) FILTER (WHERE kind='creator_reward' OR (kind='reversal' AND delta_units<0)),0),
(SELECT count(DISTINCT user_id) FROM resource_coin_votes WHERE invalidated_at IS NULL),
(SELECT count(*) FROM user_coin_accounts WHERE voting_frozen_at IS NOT NULL)
FROM coin_ledger`).Scan(&result.IssuedUnits, &result.SpentUnits, &result.RewardedUnits, &result.ActiveVoters, &result.FrozenVoters)
	return result, err
}

func (s *Store) AdminCoinLedger(ctx context.Context, userID string, limit int) ([]AdminCoinEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT l.id::text,l.user_id::text,u.username,l.delta_units,l.kind,l.reference_type,l.reference_id,l.note,l.created_at::text FROM coin_ledger l JOIN users u ON u.id=l.user_id WHERE ($1='' OR l.user_id=NULLIF($1,'')::uuid) ORDER BY l.created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AdminCoinEntry
	for rows.Next() {
		var item AdminCoinEntry
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.DeltaUnits, &item.Kind, &item.ReferenceType, &item.ReferenceID, &item.Note, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) AdminAdjustCoins(ctx context.Context, userID string, deltaUnits int64, reason, actorID string) (CoinAccount, error) {
	reason = strings.TrimSpace(reason)
	if deltaUnits == 0 || reason == "" {
		return CoinAccount{}, fmt.Errorf("coin adjustment and reason are required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return CoinAccount{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_coin_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return CoinAccount{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE user_coin_accounts SET balance_units=balance_units+$2,updated_at=now() WHERE user_id=$1 AND balance_units+$2>=0`, userID, deltaUnits)
	if err != nil {
		return CoinAccount{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return CoinAccount{}, ErrCoinBalance
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO coin_ledger(id,user_id,delta_units,kind,note,actor_id) VALUES($1,$2,$3,'admin_adjustment',$4,NULLIF($5,'')::uuid)`, uuid.NewString(), userID, deltaUnits, reason, actorID); err != nil {
		return CoinAccount{}, err
	}
	account, err := coinAccountTx(ctx, tx, userID)
	if err != nil {
		return CoinAccount{}, err
	}
	if err = tx.Commit(); err != nil {
		return CoinAccount{}, err
	}
	return account, nil
}

func (s *Store) AdminSetCoinFreeze(ctx context.Context, userID string, frozen bool, reason string) error {
	if frozen && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("freeze reason is required")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_coin_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE user_coin_accounts SET voting_frozen_at=CASE WHEN $2 THEN now() ELSE NULL END,voting_frozen_reason=CASE WHEN $2 THEN $3 ELSE '' END,updated_at=now() WHERE user_id=$1`, userID, frozen, strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

// AdminInvalidateCoinVote reverses both the voter spend and creator reward.
func (s *Store) AdminInvalidateCoinVote(ctx context.Context, resourceID, userID, reason, actorID string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("invalidation reason is required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var coins int
	var reward int64
	var ownerID string
	err = tx.QueryRowContext(ctx, `SELECT v.coins,v.creator_reward_units,r.owner_id::text FROM resource_coin_votes v JOIN resources r ON r.id=v.resource_id WHERE v.resource_id=$1 AND v.user_id=$2 AND v.invalidated_at IS NULL FOR UPDATE`, resourceID, userID).Scan(&coins, &reward, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.ErrNoRows
	}
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_coin_accounts SET balance_units=balance_units+$2,updated_at=now() WHERE user_id=$1`, userID, coins*10); err != nil {
		return err
	}
	// Fraud reversal never makes the creator balance negative. Any uncovered
	// reward remains visible in the immutable ledger for manual follow-up.
	var ownerBalance int64
	if err = tx.QueryRowContext(ctx, `SELECT balance_units FROM user_coin_accounts WHERE user_id=$1 FOR UPDATE`, ownerID).Scan(&ownerBalance); err != nil {
		return err
	}
	clawback := min(ownerBalance, reward)
	if _, err = tx.ExecContext(ctx, `UPDATE user_coin_accounts SET balance_units=balance_units-$2,updated_at=now() WHERE user_id=$1`, ownerID, clawback); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO coin_ledger(id,user_id,delta_units,kind,reference_type,reference_id,note,actor_id) VALUES($1,$2,$3,'reversal','resource',$4,$5,NULLIF($6,'')::uuid)`, uuid.NewString(), userID, coins*10, resourceID, reason, actorID); err != nil {
		return err
	}
	if clawback > 0 {
		if _, err = tx.ExecContext(ctx, `INSERT INTO coin_ledger(id,user_id,delta_units,kind,reference_type,reference_id,note,actor_id) VALUES($1,$2,$3,'reversal','resource',$4,$5,NULLIF($6,'')::uuid)`, uuid.NewString(), ownerID, -clawback, resourceID, reason, actorID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE resource_coin_votes SET invalidated_at=now(),invalidated_by=NULLIF($3,'')::uuid,invalidation_reason=$4,updated_at=now() WHERE resource_id=$1 AND user_id=$2`, resourceID, userID, actorID, reason); err != nil {
		return err
	}
	return tx.Commit()
}
