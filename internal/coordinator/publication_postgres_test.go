package coordinator

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestPublicationCoordinatorRecordsPollStateWithoutAppendingHistory(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer adminDB.Close()
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName) })
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

	ownerID, resourceID, revisionID, publicationID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,'coordinator-owner')`, ownerID, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind) VALUES($1,$2,'coordinator-history','quickapp')`, resourceID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state) VALUES($1,$2,1,'Coordinator history','','approved')`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO publications(id,revision_id,target,config) VALUES($1,$2,'astrobox','{}')`, publicationID, revisionID); err != nil {
		t.Fatal(err)
	}
	c := &Coordinator{db: db, store: store.New(db), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err := c.publishOne(ctx); err != nil {
		t.Fatalf("publishOne returned update error: %v", err)
	}
	assertPublicationEvents(t, db, publicationID, []string{"execution_started", "retry_scheduled"})

	// No GitHub grant is inserted on purpose. The poll should record the local
	// authorization error without making a network request or requiring a token.
	if _, err := db.ExecContext(ctx, `UPDATE publications SET state='reviewing',status_detail='{"pull_request_number":123}',next_attempt_at=now(),error_message='' WHERE id=$1`, publicationID); err != nil {
		t.Fatal(err)
	}
	if err := c.pollOne(ctx); err != nil {
		t.Fatalf("pollOne failed to persist transient poll state: %v", err)
	}
	assertPublicationEvents(t, db, publicationID, []string{"execution_started", "retry_scheduled"})
	var statusDetail []byte
	var nextAttempt time.Time
	if err := db.QueryRowContext(ctx, `SELECT status_detail,next_attempt_at FROM publications WHERE id=$1`, publicationID).Scan(&statusDetail, &nextAttempt); err != nil {
		t.Fatal(err)
	}
	var detail map[string]any
	if err := json.Unmarshal(statusDetail, &detail); err != nil {
		t.Fatalf("poll status is not valid JSON: %v", err)
	}
	if got, ok := detail["poll_count"].(float64); !ok || got != 1 {
		t.Fatalf("poll count = %#v, want 1", detail["poll_count"])
	}
	if _, ok := detail["last_polled_at"].(string); !ok {
		t.Fatalf("poll timestamp missing from status_detail: %s", statusDetail)
	}
	if got := detail["last_poll_error"]; got != "GitHub publishing permission is not authorized" {
		t.Fatalf("poll status was not overwritten in status_detail: %s", statusDetail)
	}
	if time.Until(nextAttempt) < 4*time.Minute {
		t.Fatalf("next poll is not scheduled five minutes later: %s", nextAttempt)
	}
	var leaseToken *string
	if err := db.QueryRowContext(ctx, `SELECT lease_token::text FROM publications WHERE id=$1`, publicationID).Scan(&leaseToken); err != nil {
		t.Fatal(err)
	}
	if leaseToken != nil {
		t.Fatalf("poll failure retained lease %q", *leaseToken)
	}
}

func assertPublicationEvents(t *testing.T, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, publicationID string, want []string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `SELECT event FROM publication_attempts WHERE publication_id=$1 ORDER BY id`, publicationID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var event string
		if err := rows.Scan(&event); err != nil {
			t.Fatal(err)
		}
		got = append(got, event)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("publication events = %v, want %v", got, want)
	}
}
