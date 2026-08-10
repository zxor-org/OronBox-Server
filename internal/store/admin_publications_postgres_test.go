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

func TestAdminPublicationHistoryLifecycle(t *testing.T) {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,$3)`, ownerID, time.Now().UnixNano(), "publication-owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind) VALUES($1,$2,'publication-history','quickapp')`, resourceID, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state) VALUES($1,$2,1,'Publication history','','approved')`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO publications(id,revision_id,target,state,attempts,error_message) VALUES($1,$2,'astrobox','failed',2,'network timeout')`, publicationID, revisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,error_message,detail) VALUES($1,2,'execute','execution_failed','running','failed','network timeout','{"request_id":"req-1"}')`, publicationID); err != nil {
		t.Fatal(err)
	}

	s := store.New(db)
	item, err := s.AdminPublication(ctx, publicationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.History) != 1 || item.History[0].Event != "execution_failed" || string(item.History[0].Detail) != `{"request_id": "req-1"}` {
		t.Fatalf("initial history = %#v", item.History)
	}
	item, err = s.AdminManagePublication(ctx, publicationID, "requeue")
	if err != nil {
		t.Fatal(err)
	}
	if item.State != "pending" || len(item.History) != 2 || item.History[0].Event != "requeued" || item.History[1].Event != "execution_failed" {
		t.Fatalf("history after requeue = state %q, %#v", item.State, item.History)
	}
	item, err = s.AdminManagePublication(ctx, publicationID, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if item.State != "cancelled" || len(item.History) != 3 || item.History[0].Event != "cancelled" {
		t.Fatalf("history after cancel = state %q, %#v", item.State, item.History)
	}
	if _, err := s.AdminManagePublication(ctx, publicationID, "cancel"); !errors.Is(err, store.ErrAdminPublicationConflict) {
		t.Fatalf("second cancel error = %v, want conflict", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM publication_attempts WHERE publication_id=$1`, publicationID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("history row count = %d, want 3", count)
	}
	if _, err := db.ExecContext(ctx, `UPDATE publication_attempts SET event='tampered' WHERE publication_id=$1`, publicationID); err == nil {
		t.Fatal("immutable publication history accepted an update")
	}

	leasedID := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO publications(id,revision_id,target,state,lease_token,lease_expires_at) VALUES($1,$2,'bandbbs','reviewing',$3,now()+interval '5 minutes')`, leasedID, revisionID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdminManagePublication(ctx, leasedID, "cancel"); !errors.Is(err, store.ErrAdminPublicationConflict) {
		t.Fatalf("cancel with active polling lease error = %v, want conflict", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE publications SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, leasedID); err != nil {
		t.Fatal(err)
	}
	leased, err := s.AdminManagePublication(ctx, leasedID, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if leased.State != "cancelled" || len(leased.History) != 1 || leased.History[0].Event != "cancelled" {
		t.Fatalf("expired lease cancellation = %#v", leased)
	}
}
