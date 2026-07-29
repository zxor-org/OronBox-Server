package store_test

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestCoinCheckinVoteAndInvalidation(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.ExecContext(context.Background(), `DROP DATABASE `+databaseName) })
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	ownerID, voterID := uuid.NewString(), uuid.NewString()
	resourceID, revisionID := uuid.NewString(), uuid.NewString()
	createdAt := time.Now().Add(-48 * time.Hour)
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username,created_at) VALUES($1,$2,'owner',$3),($4,$5,'voter',$3)`, ownerID, time.Now().UnixNano(), createdAt, voterID, time.Now().UnixNano()+1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind) VALUES($1,$2,'coin-test','quickapp')`, resourceID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state) VALUES($1,$2,1,'Coin test','','approved')`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET current_revision_id=$1 WHERE id=$2`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}

	s := store.New(db)
	checkin, err := s.CheckinCoins(ctx, voterID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.CheckinCoins(ctx, voterID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if repeated.RewardCoins != checkin.RewardCoins || repeated.Account.BalanceUnits != checkin.Account.BalanceUnits {
		t.Fatalf("repeated check-in changed reward or balance: first=%#v repeated=%#v", checkin, repeated)
	}
	if checkin.RewardCoins < 1 || checkin.RewardCoins > 5 {
		t.Fatalf("check-in reward = %d", checkin.RewardCoins)
	}
	accountBeforeVote, err := s.AdminAdjustCoins(ctx, voterID, 10, "test balance", "")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.CoinResource(ctx, voterID, resourceID, 1); err != nil {
		t.Fatal(err)
	}
	result, err := s.CoinResource(ctx, voterID, resourceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.UserCoins != 2 || result.ResourceCoins != 2 {
		t.Fatalf("coin result = %#v", result)
	}
	if _, err := s.CoinResource(ctx, voterID, resourceID, 1); !errors.Is(err, store.ErrCoinVoteLimit) {
		t.Fatalf("third coin error = %v", err)
	}
	owner, err := s.CoinAccount(ctx, ownerID)
	if err != nil || owner.BalanceUnits != 2 {
		t.Fatalf("creator reward account = %#v, error=%v", owner, err)
	}

	if err := s.AdminInvalidateCoinVote(ctx, resourceID, voterID, "test reversal", ""); err != nil {
		t.Fatal(err)
	}
	voter, err := s.CoinAccount(ctx, voterID)
	if err != nil || voter.BalanceUnits != accountBeforeVote.BalanceUnits {
		t.Fatalf("reversed voter account = %#v, error=%v", voter, err)
	}
	owner, err = s.CoinAccount(ctx, ownerID)
	if err != nil || owner.BalanceUnits != 0 {
		t.Fatalf("reversed creator account = %#v, error=%v", owner, err)
	}
}
