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
	if _, err := s.CreateComment(ctx, resourceID, replierID, reply.ID, "nested", "visible"); err != nil {
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
	var hiddenReplyMessages, hiddenModerationMessages int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE ref=$1 AND kind='comment_reply'`, hiddenReply.ID).Scan(&hiddenReplyMessages); err != nil {
		t.Fatal(err)
	}
	if hiddenReplyMessages != 0 {
		t.Fatalf("hidden reply parent notifications = %d, want 0 until the reply is visible", hiddenReplyMessages)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE ref=$1 AND kind='moderation'`, hiddenReply.ID).Scan(&hiddenModerationMessages); err != nil {
		t.Fatal(err)
	}
	if hiddenModerationMessages != 0 {
		t.Fatalf("hidden reply author notifications = %d, want 0 for pending shadow visibility", hiddenModerationMessages)
	}

	for _, viewer := range []string{"", userID, replierID} {
		page, err := s.CommentRoots(ctx, resourceID, store.CommunityCursor{}, 50, viewer)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, root := range page.Items {
			if root.ID == top.ID {
				for _, reply := range root.Replies {
					if reply.ID == hiddenReply.ID {
						found = true
					}
				}
				if viewer == replierID && root.ReplyCount != 3 {
					t.Fatalf("owner reply count=%d", root.ReplyCount)
				}
				if viewer != replierID && root.ReplyCount != 2 {
					t.Fatalf("public reply count=%d", root.ReplyCount)
				}
			}
		}
		if found != (viewer == replierID) {
			t.Fatalf("viewer=%q pending preview visible=%v", viewer, found)
		}
		thread, err := s.CommentThread(ctx, hiddenReply.ID, store.CommunityCursor{}, store.CommunityCursor{}, 1, viewer)
		if viewer != replierID {
			if !errors.Is(err, store.ErrCommentNotFound) {
				t.Fatalf("pending reply leaked: %v", err)
			}
		} else if err != nil || len(thread.Replies) != 1 || thread.Replies[0].ID != hiddenReply.ID {
			t.Fatalf("owner cannot reach pending reply: %+v %v", thread, err)
		}
	}

	pendingRoot, err := s.CreateModeratedComment(ctx, resourceID, userID, "", "pending root", "hidden", &store.CommentModerationRecord{Action: "review"})
	if err != nil {
		t.Fatal(err)
	}
	blockedRoot, err := s.CreateModeratedComment(ctx, resourceID, userID, "", "blocked root", "hidden", &store.CommentModerationRecord{Action: "block"})
	if err != nil {
		t.Fatal(err)
	}
	for _, viewer := range []string{"", userID, replierID} {
		_, err := s.CommentThread(ctx, pendingRoot.ID, store.CommunityCursor{}, store.CommunityCursor{}, 20, viewer)
		if viewer == userID {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, store.ErrCommentNotFound) {
			t.Fatalf("pending root leaked: %v", err)
		}
		if _, err := s.CommentThread(ctx, blockedRoot.ID, store.CommunityCursor{}, store.CommunityCursor{}, 20, viewer); !errors.Is(err, store.ErrCommentNotFound) {
			t.Fatalf("blocked root leaked: %v", err)
		}
	}
	if _, err := s.CreateComment(ctx, resourceID, userID, pendingRoot.ID, "own pending reply", "visible"); err != nil {
		t.Fatalf("cannot reply to own pending root: %v", err)
	}
	if _, err := s.CreateComment(ctx, resourceID, replierID, pendingRoot.ID, "other reply", "visible"); !errors.Is(err, store.ErrCommentNotFound) {
		t.Fatalf("other user could reply to hidden root: %v", err)
	}
	if err := s.AdminModerateComment(ctx, pendingRoot.ID, "hide", replierID, "review rejected"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommentThread(ctx, pendingRoot.ID, store.CommunityCursor{}, store.CommunityCursor{}, 20, userID); !errors.Is(err, store.ErrCommentNotFound) {
		t.Fatalf("human rejected comment still visible to author: %v", err)
	}
	if err := s.AdminModerateComment(ctx, hiddenReply.ID, "approve", replierID, "复核后确认无问题"); err != nil {
		t.Fatal(err)
	}
	// A moderation decision has to name the person who made it and why, so a
	// disputed removal can be traced to someone rather than to "the system".
	var decidedBy, decidedNote string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(human_reviewer_id::text,''),human_note FROM comment_moderation WHERE comment_id=$1`, hiddenReply.ID).Scan(&decidedBy, &decidedNote); err != nil {
		t.Fatal(err)
	}
	if decidedBy != replierID || decidedNote != "复核后确认无问题" {
		t.Fatalf("moderation decision recorded reviewer %q note %q, want the acting admin and their reason", decidedBy, decidedNote)
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
	if len(items) != 4 {
		t.Fatalf("unexpected visible comments: %#v", items)
	}
	for _, item := range items {
		if item.ID == top.ID || item.ID == reply.ID || item.ID == hiddenReply.ID {
			t.Fatalf("deleted thread remained visible: %#v", items)
		}
	}
	var replyMessages int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND kind='comment_reply' AND ref=$2`, userID, reply.ID).Scan(&replyMessages); err != nil {
		t.Fatal(err)
	}
	if replyMessages != 1 {
		t.Fatalf("reply messages = %d, want 1", replyMessages)
	}
}
