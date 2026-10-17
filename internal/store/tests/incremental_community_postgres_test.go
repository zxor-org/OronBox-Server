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

func TestIncrementalNestedRepliesAndCreatorNotification(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminDB, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { adminDB.Close() })
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := adminDB.ExecContext(context.Background(), `DROP DATABASE `+databaseName); err != nil {
			t.Errorf("cleanup test database: %v", err)
		}
	})
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

	top, err := s.CreateComment(ctx, resourceID, replierID, "", "root", "visible")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND data->>'comment_id'=$2`, userID, top.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("creator notifications=%d, want 1", count)
	}
	reply, err := s.CreateComment(ctx, resourceID, userID, top.ID, "reply", "visible")
	if err != nil {
		t.Fatal(err)
	}
	nested, err := s.CreateComment(ctx, resourceID, replierID, reply.ID, "nested reply", "visible")
	if err != nil {
		t.Fatalf("reply to reply rejected: %v", err)
	}
	if nested.ParentID != top.ID {
		t.Fatalf("root parent=%s want %s", nested.ParentID, top.ID)
	}
	pending, err := s.CreateComment(ctx, resourceID, replierID, "", "pending creator notification", "hidden")
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"approve", "hide", "approve"} {
		if err := s.AdminModerateComment(ctx, pending.ID, action, userID, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND event='comment.created' AND data->>'comment_id'=$2`, userID, pending.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("moderation replay sent %d creator notifications", count)
	}

	// Pagination uses the complete timestamp+UUID tuple, including ties, and has no 100-message ceiling.
	if _, err := db.ExecContext(ctx, `INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref,created_at) SELECT gen_random_uuid(),$1,'comment_reply','test','{}','','','', '2026-01-01'::timestamptz FROM generate_series(1,205)`, userID); err != nil {
		t.Fatal(err)
	}
	cursor := store.CommunityCursor{}
	seen := map[string]bool{}
	for i := 0; i < 30; i++ {
		page, err := s.UserMessagePage(ctx, userID, cursor, 20, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.Items {
			if seen[m.ID] {
				t.Fatal("duplicate message across pages")
			}
			seen[m.ID] = true
		}
		if !page.HasMore {
			break
		}
		cursor, err = store.DecodeCommunityCursor(page.NextCursor)
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) < 205 {
		t.Fatalf("only %d messages reachable", len(seen))
	}
	for id := range seen {
		if _, err := db.ExecContext(ctx, `UPDATE user_messages SET read_at=now() WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		break
	}
	unreadPage, err := s.UserMessagePage(ctx, userID, store.CommunityCursor{}, 20, true)
	if err != nil {
		t.Fatal(err)
	}
	if unreadPage.Unread != len(seen)-1 {
		t.Fatalf("unread count=%d want %d", unreadPage.Unread, len(seen)-1)
	}
	for _, m := range unreadPage.Items {
		if m.ReadAt != nil {
			t.Fatal("read message leaked into unread-only page")
		}
	}

	// A notification can resolve a reply outside the first page directly.
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_comments(id,resource_id,user_id,parent_id,reply_to_id,body) SELECT gen_random_uuid(),$1,$2,$3,$3,'reply-'||i FROM generate_series(1,45) i`, resourceID, userID, top.ID); err != nil {
		t.Fatal(err)
	}
	var target string
	if err := db.QueryRowContext(ctx, `SELECT id::text FROM resource_comments WHERE parent_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, top.ID).Scan(&target); err != nil {
		t.Fatal(err)
	}
	thread, err := s.CommentThread(ctx, target, store.CommunityCursor{}, store.CommunityCursor{}, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range thread.Replies {
		if r.ID == target {
			found = true
		}
	}
	if !found || thread.Root.ID != top.ID {
		t.Fatal("notification target is absent from thread page")
	}
	if thread.PreviousCursor == "" {
		t.Fatal("missing earlier reply cursor")
	}
	if _, err := db.ExecContext(ctx, `UPDATE resource_comments SET moderation_state='hidden' WHERE id=$1`, top.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommentThread(ctx, target, store.CommunityCursor{}, store.CommunityCursor{}, 20); err != store.ErrCommentNotFound {
		t.Fatalf("hidden root leaked: %v", err)
	}

	// Recreate the v7 shape to exercise a real additive upgrade, then migrate twice.
	if _, err := db.ExecContext(ctx, `DROP INDEX comments_thread_cursor_idx;DROP INDEX messages_user_cursor_idx;ALTER TABLE resource_comments DROP COLUMN reply_to_id,DROP COLUMN notified_at;DELETE FROM schema_migrations WHERE version=8;INSERT INTO schema_migrations(version) VALUES(7) ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("v7 upgrade: %v", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("idempotent migration: %v", err)
	}

}
