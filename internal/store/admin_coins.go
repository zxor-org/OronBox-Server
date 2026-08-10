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

type AdminCoinQuery struct {
	Search, User, Kind, ReferenceType, Sort string
	From, To                                *time.Time
	Page, PerPage                           int
}

type AdminCoinPage struct {
	Items                            []AdminCoinEntry
	Total, Page, PerPage, TotalPages int
	Query                            AdminCoinQuery
}

func (q AdminCoinQuery) normalized() AdminCoinQuery {
	q.Search, q.User, q.Kind = strings.TrimSpace(q.Search), strings.TrimSpace(q.User), strings.TrimSpace(q.Kind)
	q.ReferenceType = strings.TrimSpace(q.ReferenceType)
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 {
		q.PerPage = 25
	}
	if q.PerPage > 100 {
		q.PerPage = 100
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		q.From, q.To = q.To, q.From
	}
	switch q.Sort {
	case "oldest", "delta_desc", "delta_asc":
	default:
		q.Sort = "newest"
	}
	return q
}

func adminCoinOrder(sort string) string {
	switch sort {
	case "oldest":
		return "l.created_at ASC,l.id ASC"
	case "delta_desc":
		return "l.delta_units DESC,l.created_at DESC"
	case "delta_asc":
		return "l.delta_units ASC,l.created_at DESC"
	default:
		return "l.created_at DESC,l.id DESC"
	}
}

func (s *Store) AdminCoinLedgerPage(ctx context.Context, raw AdminCoinQuery) (AdminCoinPage, error) {
	q := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',u.username,u.id::text,l.note,l.kind,l.reference_type,l.reference_id,l.id::text) ILIKE '%'||$1||'%') AND ($2='' OR u.id::text=$2 OR u.username ILIKE '%'||$2||'%') AND ($3='' OR l.kind=$3) AND ($4='' OR l.reference_type=$4) AND ($5::timestamptz IS NULL OR l.created_at >= $5) AND ($6::timestamptz IS NULL OR l.created_at <= $6)`
	page := AdminCoinPage{Items: []AdminCoinEntry{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	args := []any{q.Search, q.User, q.Kind, q.ReferenceType, q.From, q.To}
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM coin_ledger l JOIN users u ON u.id=l.user_id WHERE `+filter, args...).Scan(&page.Total); err != nil {
		return AdminCoinPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT l.id::text,l.user_id::text,u.username,l.delta_units,l.kind,l.reference_type,l.reference_id,l.note,l.created_at::text FROM coin_ledger l JOIN users u ON u.id=l.user_id WHERE %s ORDER BY %s LIMIT $7 OFFSET $8`, filter, adminCoinOrder(q.Sort)), append(args, q.PerPage, (q.Page-1)*q.PerPage)...)
	if err != nil {
		return AdminCoinPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AdminCoinEntry
		if err := rows.Scan(&item.ID, &item.UserID, &item.Username, &item.DeltaUnits, &item.Kind, &item.ReferenceType, &item.ReferenceID, &item.Note, &item.CreatedAt); err != nil {
			return AdminCoinPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminCoinPage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

type AdminCoinUserOption struct {
	ID, Username string
	BalanceUnits int64
	Frozen       bool
}

func (s *Store) AdminCoinUserOptions(ctx context.Context, search string, limit int) ([]AdminCoinUserOption, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	search = strings.TrimSpace(search)
	rows, err := s.db.QueryContext(ctx, `SELECT u.id::text,u.username,COALESCE(a.balance_units,0),a.voting_frozen_at IS NOT NULL FROM users u LEFT JOIN user_coin_accounts a ON a.user_id=u.id WHERE $1='' OR u.username ILIKE '%'||$1||'%' OR u.id::text=$1 ORDER BY u.username LIMIT $2`, search, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AdminCoinUserOption{}
	for rows.Next() {
		var item AdminCoinUserOption
		if err := rows.Scan(&item.ID, &item.Username, &item.BalanceUnits, &item.Frozen); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
