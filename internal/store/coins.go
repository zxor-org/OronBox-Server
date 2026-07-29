package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCoinBalance      = errors.New("insufficient coin balance")
	ErrCoinVoteLimit    = errors.New("resource coin limit reached")
	ErrCoinVoteOwn      = errors.New("cannot coin your own resource")
	ErrCoinVotingFrozen = errors.New("coin voting is frozen")
	ErrCoinAccountYoung = errors.New("account must be at least 24 hours old")
)

type CoinAccount struct {
	BalanceUnits       int64      `json:"balance_units"`
	Balance            float64    `json:"balance"`
	VotingFrozenAt     *time.Time `json:"voting_frozen_at,omitempty"`
	VotingFrozenReason string     `json:"voting_frozen_reason,omitempty"`
}

type CoinCheckin struct {
	Date        string      `json:"date"`
	RewardCoins int         `json:"reward_coins"`
	Account     CoinAccount `json:"account"`
}

type ResourceCoinResult struct {
	ResourceID    string      `json:"resource_id"`
	UserCoins     int         `json:"user_coins"`
	ResourceCoins int64       `json:"resource_coins"`
	Account       CoinAccount `json:"account"`
}

type CreatorCoinStats struct {
	LifetimeCoins int64   `json:"lifetime_coins"`
	RecentCoins   int64   `json:"recent_14d_coins"`
	RewardUnits   int64   `json:"reward_units"`
	Reward        float64 `json:"reward"`
}

func (s *Store) CreatorCoinStats(ctx context.Context, userID string) (CreatorCoinStats, error) {
	var result CreatorCoinStats
	err := s.db.QueryRowContext(ctx, `SELECT
COALESCE((SELECT sum(vote.coins) FROM resource_coin_votes vote JOIN resources resource ON resource.id=vote.resource_id WHERE resource.owner_id=$1 AND vote.invalidated_at IS NULL),0),
COALESCE((SELECT sum(-ledger.delta_units/10) FROM coin_ledger ledger JOIN resource_coin_votes vote ON vote.resource_id::text=ledger.reference_id AND vote.user_id=ledger.user_id AND vote.invalidated_at IS NULL JOIN resources resource ON resource.id=vote.resource_id WHERE resource.owner_id=$1 AND ledger.kind='resource_vote' AND ledger.created_at>=now()-interval '14 days'),0),
COALESCE((SELECT sum(vote.creator_reward_units) FROM resource_coin_votes vote JOIN resources resource ON resource.id=vote.resource_id WHERE resource.owner_id=$1 AND vote.invalidated_at IS NULL),0)`, userID).
		Scan(&result.LifetimeCoins, &result.RecentCoins, &result.RewardUnits)
	result.Reward = float64(result.RewardUnits) / 10
	return result, err
}

func coinAccount(units int64, frozenAt sql.NullTime, reason string) CoinAccount {
	account := CoinAccount{BalanceUnits: units, Balance: float64(units) / 10, VotingFrozenReason: reason}
	if frozenAt.Valid {
		account.VotingFrozenAt = &frozenAt.Time
	}
	return account
}

func (s *Store) CoinAccount(ctx context.Context, userID string) (CoinAccount, error) {
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_coin_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return CoinAccount{}, err
	}
	var units int64
	var frozenAt sql.NullTime
	var reason string
	if err := s.db.QueryRowContext(ctx, `SELECT balance_units,voting_frozen_at,voting_frozen_reason FROM user_coin_accounts WHERE user_id=$1`, userID).Scan(&units, &frozenAt, &reason); err != nil {
		return CoinAccount{}, err
	}
	return coinAccount(units, frozenAt, reason), nil
}

func (s *Store) CheckinCoins(ctx context.Context, userID string, now time.Time) (CoinCheckin, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return CoinCheckin{}, err
	}
	defer tx.Rollback()
	day := now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format("2006-01-02")
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_coin_accounts(user_id) VALUES($1) ON CONFLICT(user_id) DO NOTHING`, userID); err != nil {
		return CoinCheckin{}, err
	}
	var rewardUnits int
	err = tx.QueryRowContext(ctx, `SELECT reward_units FROM daily_coin_checkins WHERE user_id=$1 AND checkin_date=$2::date`, userID, day).Scan(&rewardUnits)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return CoinCheckin{}, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		reward, randomErr := weightedDailyCoins()
		if randomErr != nil {
			return CoinCheckin{}, randomErr
		}
		rewardUnits = reward * 10
		ledgerID := uuid.NewString()
		if _, err = tx.ExecContext(ctx, `UPDATE user_coin_accounts SET balance_units=balance_units+$2,updated_at=now() WHERE user_id=$1`, userID, rewardUnits); err != nil {
			return CoinCheckin{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO coin_ledger(id,user_id,delta_units,kind,reference_type,reference_id) VALUES($1,$2,$3,'checkin','date',$4)`, ledgerID, userID, rewardUnits, day); err != nil {
			return CoinCheckin{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO daily_coin_checkins(user_id,checkin_date,reward_units,ledger_id) VALUES($1,$2::date,$3,$4)`, userID, day, rewardUnits, ledgerID); err != nil {
			return CoinCheckin{}, err
		}
	}
	account, err := coinAccountTx(ctx, tx, userID)
	if err != nil {
		return CoinCheckin{}, err
	}
	if err = tx.Commit(); err != nil {
		return CoinCheckin{}, err
	}
	return CoinCheckin{Date: day, RewardCoins: rewardUnits / 10, Account: account}, nil
}

func weightedDailyCoins() (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(15))
	if err != nil {
		return 0, fmt.Errorf("generate coin checkin reward: %w", err)
	}
	switch n := value.Int64(); {
	case n < 5:
		return 1, nil
	case n < 9:
		return 2, nil
	case n < 12:
		return 3, nil
	case n < 14:
		return 4, nil
	default:
		return 5, nil
	}
}

func coinAccountTx(ctx context.Context, tx *sql.Tx, userID string) (CoinAccount, error) {
	var units int64
	var frozenAt sql.NullTime
	var reason string
	err := tx.QueryRowContext(ctx, `SELECT balance_units,voting_frozen_at,voting_frozen_reason FROM user_coin_accounts WHERE user_id=$1`, userID).Scan(&units, &frozenAt, &reason)
	return coinAccount(units, frozenAt, reason), err
}

func (s *Store) CoinResource(ctx context.Context, userID, resourceID string, coins int) (ResourceCoinResult, error) {
	if coins < 1 || coins > 2 {
		return ResourceCoinResult{}, ErrCoinVoteLimit
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ResourceCoinResult{}, err
	}
	defer tx.Rollback()
	var userCreatedAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT created_at FROM users WHERE id=$1`, userID).Scan(&userCreatedAt); err != nil {
		return ResourceCoinResult{}, err
	}
	if time.Since(userCreatedAt) < 24*time.Hour {
		return ResourceCoinResult{}, ErrCoinAccountYoung
	}
	var ownerID string
	err = tx.QueryRowContext(ctx, `SELECT owner_id::text FROM resources WHERE id=$1 AND moderation_state='visible' AND current_revision_id IS NOT NULL FOR UPDATE`, resourceID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ResourceCoinResult{}, ErrAdminResourceNotFound
	}
	if err != nil {
		return ResourceCoinResult{}, err
	}
	if ownerID == userID {
		return ResourceCoinResult{}, ErrCoinVoteOwn
	}
	var collaborator bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resource_collaborators WHERE resource_id=$1 AND user_id=$2 AND accepted_at IS NOT NULL)`, resourceID, userID).Scan(&collaborator); err != nil {
		return ResourceCoinResult{}, err
	}
	if collaborator {
		return ResourceCoinResult{}, ErrCoinVoteOwn
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_coin_accounts(user_id) VALUES($1),($2) ON CONFLICT(user_id) DO NOTHING`, userID, ownerID); err != nil {
		return ResourceCoinResult{}, err
	}
	account, err := coinAccountTx(ctx, tx, userID)
	if err != nil {
		return ResourceCoinResult{}, err
	}
	if account.VotingFrozenAt != nil {
		return ResourceCoinResult{}, ErrCoinVotingFrozen
	}
	var existing int
	err = tx.QueryRowContext(ctx, `SELECT coins FROM resource_coin_votes WHERE resource_id=$1 AND user_id=$2 AND invalidated_at IS NULL FOR UPDATE`, resourceID, userID).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ResourceCoinResult{}, err
	}
	if existing+coins > 2 {
		return ResourceCoinResult{}, ErrCoinVoteLimit
	}
	spendUnits := int64(coins * 10)
	if account.BalanceUnits < spendUnits {
		return ResourceCoinResult{}, ErrCoinBalance
	}
	rewardUnits := coins
	if _, err = tx.ExecContext(ctx, `UPDATE user_coin_accounts SET balance_units=balance_units-$2,updated_at=now() WHERE user_id=$1`, userID, spendUnits); err != nil {
		return ResourceCoinResult{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE user_coin_accounts SET balance_units=balance_units+$2,updated_at=now() WHERE user_id=$1`, ownerID, rewardUnits); err != nil {
		return ResourceCoinResult{}, err
	}
	voteLedgerID, rewardLedgerID := uuid.NewString(), uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO coin_ledger(id,user_id,delta_units,kind,reference_type,reference_id) VALUES($1,$2,$3,'resource_vote','resource',$4),($5,$6,$7,'creator_reward','resource',$4)`, voteLedgerID, userID, -spendUnits, resourceID, rewardLedgerID, ownerID, rewardUnits); err != nil {
		return ResourceCoinResult{}, err
	}
	if existing == 0 {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `INSERT INTO resource_coin_votes(resource_id,user_id,coins,creator_reward_units) VALUES($1,$2,$3,$4)
ON CONFLICT(resource_id,user_id) DO UPDATE SET coins=EXCLUDED.coins,creator_reward_units=EXCLUDED.creator_reward_units,invalidated_at=NULL,invalidated_by=NULL,invalidation_reason='',created_at=now(),updated_at=now()
WHERE resource_coin_votes.invalidated_at IS NOT NULL`, resourceID, userID, coins, rewardUnits)
		if err == nil {
			if count, _ := result.RowsAffected(); count != 1 {
				err = ErrCoinVoteLimit
			}
		}
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE resource_coin_votes SET coins=coins+$3,creator_reward_units=creator_reward_units+$4,updated_at=now() WHERE resource_id=$1 AND user_id=$2`, resourceID, userID, coins, rewardUnits)
	}
	if err != nil {
		return ResourceCoinResult{}, err
	}
	account, err = coinAccountTx(ctx, tx, userID)
	if err != nil {
		return ResourceCoinResult{}, err
	}
	var total int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(coins),0) FROM resource_coin_votes WHERE resource_id=$1 AND invalidated_at IS NULL`, resourceID).Scan(&total); err != nil {
		return ResourceCoinResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return ResourceCoinResult{}, err
	}
	return ResourceCoinResult{ResourceID: resourceID, UserCoins: existing + coins, ResourceCoins: total, Account: account}, nil
}
