package store_test

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

func TestCommentRateHierarchyAndSoftDelete(t *testing.T) {
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
	parsed, _ := url.Parse(databaseURL)
	parsed.Path = "/" + databaseName
	db, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	userID, replierID, resourceID, revisionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,'commenter')`, userID, time.Now().UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users(id,bandbbs_user_id,username) VALUES($1,$2,'replier')`, replierID, time.Now().UnixNano()+1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resources(id,owner_id,slug,kind) VALUES($1,$2,'comments','quickapp')`, resourceID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_revisions(id,resource_id,revision_no,name,summary,state) VALUES($1,$2,1,'Comments','Summary','approved')`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resources SET current_revision_id=$1 WHERE id=$2`, revisionID, resourceID); err != nil {
		t.Fatal(err)
	}
	s := store.New(db)
	top, err := s.CreateComment(ctx, resourceID, userID, "", "top", "visible")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4; index++ {
		if _, err := s.CreateComment(ctx, resourceID, userID, "", fmt.Sprintf("burst-%d", index), "visible"); err != nil {
			t.Fatalf("burst comment %d: %v", index, err)
		}
	}
	if _, err := s.CreateComment(ctx, resourceID, userID, "", "limited", "visible"); !errors.Is(err, store.ErrCommentTooFast) {
		t.Fatalf("sixth comment error = %v", err)
	}
	_, _ = db.ExecContext(ctx, `UPDATE resource_comments SET created_at=created_at-interval '2 minutes' WHERE user_id=$1`, userID)
	reply, err := s.CreateComment(ctx, resourceID, replierID, top.ID, "reply", "visible")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx, `UPDATE resource_comments SET created_at=created_at-interval '10 seconds' WHERE id=$1`, reply.ID)
	if _, err := s.CreateComment(ctx, resourceID, replierID, reply.ID, "nested", "visible"); !errors.Is(err, store.ErrCommentNotFound) {
		t.Fatalf("nested reply error = %v", err)
	}
	hiddenReply, err := s.CreateModeratedComment(ctx, resourceID, replierID, top.ID, "pending reply", "hidden", &store.CommentModerationRecord{
		Provider: "test",
		Model:    "test",
		Action:   "review",
		Raw:      map[string]any{"result": "review"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var hiddenReplyMessages int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE ref=$1`, hiddenReply.ID).Scan(&hiddenReplyMessages); err != nil {
		t.Fatal(err)
	}
	if hiddenReplyMessages != 0 {
		t.Fatalf("hidden reply messages = %d, want 0", hiddenReplyMessages)
	}
	if err := s.AdminModerateComment(ctx, hiddenReply.ID, "approve"); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE ref=$1 AND kind='comment_reply'`, hiddenReply.ID).Scan(&hiddenReplyMessages); err != nil {
		t.Fatal(err)
	}
	if hiddenReplyMessages != 1 {
		t.Fatalf("approved reply messages = %d, want 1", hiddenReplyMessages)
	}
	if err := s.SoftDeleteComment(ctx, reply.ID, userID); !errors.Is(err, store.ErrCommentNotFound) {
		t.Fatalf("non-owner delete error = %v, want ErrCommentNotFound", err)
	}
	if err := s.AdminDeleteComment(ctx, reply.ID); err != nil {
		t.Fatalf("privileged delete: %v", err)
	}
	if err := s.SoftDeleteComment(ctx, top.ID, userID); err != nil {
		t.Fatal(err)
	}
	items, err := s.ListComments(ctx, resourceID, userID, time.Now().Add(time.Minute), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("unexpected thread: %#v", items)
	}
	var replyMessages int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND kind='comment_reply' AND ref=$2`, userID, reply.ID).Scan(&replyMessages); err != nil {
		t.Fatal(err)
	}
	if replyMessages != 1 {
		t.Fatalf("reply messages = %d, want 1", replyMessages)
	}
}
