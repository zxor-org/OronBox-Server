package integration_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zxor-org/OronBox-Server/internal/store"
)

// Integration tests run against a real PostgreSQL. They skip gracefully when
// TEST_DATABASE_URL is unset so `go test ./...` stays green without a DB.
// Local verification uses an ephemeral PostgreSQL instance; production
// Compose pins postgres:18.6-alpine. The DDL under test only uses features
// common to both (pgcrypto, uuid, jsonb, timestamptz).

func integrationURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping PostgreSQL integration test")
	}
	return url
}

func TestOpenMigrateAndCommitParity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	s, err := store.Open(ctx, integrationURL(t))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer s.Close()

	if err := store.Migrate(ctx, s); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Idempotent: second run must also succeed.
	if err := store.Migrate(ctx, s); err != nil {
		t.Fatalf("re-migrate (idempotency): %v", err)
	}
	if err := store.Schema(ctx, s); err != nil {
		t.Fatalf("schema alias: %v", err)
	}

	var tables int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('users','oauth_states','oauth_events','oauth_grants','login_tickets','sessions','admin_sessions','github_grants','resources','resource_sources','resource_ratings','resource_collections','resource_collection_revisions','tags','resource_comments','resource_coin_votes','resource_revisions','resource_collaborators','publications','publication_attempts','external_bindings','review_cases','review_case_events','review_appeals','moderation_reason_templates','user_messages','user_coin_accounts','daily_coin_checkins','coin_ledger','feedback_tickets','feedback_replies','feedback_internal_notes','feedback_status_history','plugins','plugin_versions','app_releases','download_events','audit_logs','server_settings','blobs','blob_replicas')`).Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 41 {
		t.Fatalf("expected 41 core tables, got %d", tables)
	}

	// Commit path: two users + resource + coin parity flow mirroring api.go tip logic.
	var alice, bob int
	err = s.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO users(bandbbs_uid,username) VALUES(91001,'alice') ON CONFLICT(bandbbs_uid) DO UPDATE SET username=EXCLUDED.username RETURNING bandbbs_uid`).Scan(&alice); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO users(bandbbs_uid,username) VALUES(91002,'bob') ON CONFLICT(bandbbs_uid) DO UPDATE SET username=EXCLUDED.username RETURNING bandbbs_uid`).Scan(&bob); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO resources(resource_id,title,summary,owner_uid,coin_count) VALUES('com.example.coverage','Coverage','Coverage test',$1,0) ON CONFLICT(resource_id) DO UPDATE SET owner_uid=EXCLUDED.owner_uid`, bob); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_coin_accounts(user_uid,balance) VALUES($1,100) ON CONFLICT(user_uid) DO UPDATE SET balance=100,updated_at=now()`, alice); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_coin_accounts(user_uid,balance) VALUES($1,0) ON CONFLICT(user_uid) DO UPDATE SET balance=0,updated_at=now()`, bob); err != nil {
			return err
		}
		// Tip 10 units: sender -10, owner +1 (10%), burn tracked implicitly.
		var balance int64
		if err := tx.QueryRow(ctx, `SELECT balance FROM user_coin_accounts WHERE user_uid=$1 FOR UPDATE`, alice).Scan(&balance); err != nil {
			return err
		}
		if balance < 10 {
			return errors.New("insufficient balance")
		}
		if _, err := tx.Exec(ctx, `UPDATE user_coin_accounts SET balance=balance-10,updated_at=now() WHERE user_uid=$1`, alice); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE user_coin_accounts SET balance=balance+1,updated_at=now() WHERE user_uid=$1`, bob); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO resource_coin_votes(resource_id,user_uid,amount) VALUES('com.example.coverage',$1,10) ON CONFLICT(resource_id,user_uid) DO UPDATE SET amount=resource_coin_votes.amount+EXCLUDED.amount,updated_at=now()`, alice); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE resources SET coin_count=coin_count+10,updated_at=now() WHERE resource_id='com.example.coverage'`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO coin_ledger(user_uid,delta_units,kind,reference_type,reference_id,note,balance_after) VALUES($1,-10,'tip_send','resource','com.example.coverage','tip',$2)`, alice, balance-10); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO coin_ledger(user_uid,delta_units,kind,reference_type,reference_id,note,balance_after) VALUES($1,1,'tip_receive','resource','com.example.coverage','reward',1)`, bob)
		return err
	})
	if err != nil {
		t.Fatalf("commit parity transaction: %v", err)
	}

	var aliceBalance, bobBalance, coinCount int64
	if err := s.Pool.QueryRow(ctx, `SELECT balance FROM user_coin_accounts WHERE user_uid=$1`, alice).Scan(&aliceBalance); err != nil {
		t.Fatalf("alice balance: %v", err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT balance FROM user_coin_accounts WHERE user_uid=$1`, bob).Scan(&bobBalance); err != nil {
		t.Fatalf("bob balance: %v", err)
	}
	if err := s.Pool.QueryRow(ctx, `SELECT coin_count FROM resources WHERE resource_id='com.example.coverage'`).Scan(&coinCount); err != nil {
		t.Fatalf("coin count: %v", err)
	}
	if aliceBalance != 90 || bobBalance != 1 || coinCount < 10 {
		t.Fatalf("parity mismatch: alice=%d bob=%d coins=%d", aliceBalance, bobBalance, coinCount)
	}
}

func TestWithTxRollbackLeavesNoTrace(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	s, err := store.Open(ctx, integrationURL(t))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer s.Close()
	if err := store.Migrate(ctx, s); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	forced := errors.New("forced rollback for coverage")
	err = s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, execErr := tx.Exec(ctx, `INSERT INTO users(bandbbs_uid,username) VALUES(91999,'rollback-probe') ON CONFLICT DO NOTHING`); execErr != nil {
			return execErr
		}
		return forced
	})
	if !errors.Is(err, forced) {
		t.Fatalf("expected forced rollback error, got %v", err)
	}
	var n int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE bandbbs_uid=91999`).Scan(&n); err != nil {
		t.Fatalf("probe count: %v", err)
	}
	if n != 0 {
		t.Fatalf("rollback leaked %d rows", n)
	}
}
