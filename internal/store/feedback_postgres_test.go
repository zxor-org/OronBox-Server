package store_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestFeedbackAdminHistoryNotesAndTargetSnapshot(t *testing.T) {
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

	reporterID, ownerID, adminID, resourceID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	seed := time.Now().UnixNano()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,'reporter'),($3,$4,'owner'),($5,$6,'moderator')`, reporterID, seed, ownerID, seed+1, adminID, seed+2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind) VALUES($1,$2,$3,'watchface')`, resourceID, ownerID, "reported-"+resourceID); err != nil {
		t.Fatal(err)
	}
	feedbackStore := store.New(db)
	ticket, err := feedbackStore.CreateFeedback(ctx, store.CreateFeedbackParams{UserID: reporterID, Kind: store.FeedbackKindResourceReport, Subject: "侵权", Message: "请核查", TargetSource: "oronbox", TargetID: resourceID})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := feedbackStore.AdminFeedbackDetail(ctx, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TargetSnapshot.Kind != "resource" || detail.TargetSnapshot.Owner != "owner" || detail.TargetSnapshot.ID != resourceID {
		t.Fatalf("target snapshot = %#v", detail.TargetSnapshot)
	}
	if len(detail.StatusHistory) != 1 || detail.StatusHistory[0].ToStatus != "open" {
		t.Fatalf("initial history = %#v", detail.StatusHistory)
	}
	if _, err := feedbackStore.UpdateFeedback(ctx, ticket.ID, store.FeedbackUpdate{Status: "investigating", Reply: "正在处理", AuthorID: adminID}); err != nil {
		t.Fatal(err)
	}
	if _, err := feedbackStore.AddFeedbackInternalNote(ctx, ticket.ID, adminID, "核验来源授权"); err != nil {
		t.Fatal(err)
	}
	detail, err = feedbackStore.AdminFeedbackDetail(ctx, ticket.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Ticket.Replies) != 1 || detail.Ticket.Replies[0].Message != "正在处理" {
		t.Fatalf("public replies = %#v", detail.Ticket.Replies)
	}
	if len(detail.InternalNotes) != 1 || detail.InternalNotes[0].Message != "核验来源授权" {
		t.Fatalf("internal notes = %#v", detail.InternalNotes)
	}
	if len(detail.StatusHistory) != 2 || detail.StatusHistory[1].FromStatus != "open" || detail.StatusHistory[1].ToStatus != "investigating" || detail.StatusHistory[1].Actor != "moderator" {
		t.Fatalf("status history = %#v", detail.StatusHistory)
	}
	userView, err := feedbackStore.Feedback(ctx, ticket.ID, reporterID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(userView.Replies) != 1 {
		t.Fatalf("user-visible replies = %#v", userView.Replies)
	}
}
