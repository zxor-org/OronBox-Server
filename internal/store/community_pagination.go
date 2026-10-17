package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type CommunityCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func EncodeCommunityCursor(at time.Time, id string) string {
	b, _ := json.Marshal(CommunityCursor{at, id})
	return base64.RawURLEncoding.EncodeToString(b)
}
func DecodeCommunityCursor(raw string) (CommunityCursor, error) {
	var c CommunityCursor
	if raw == "" {
		return c, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return c, fmt.Errorf("invalid cursor")
	}
	if json.Unmarshal(b, &c) != nil || c.At.IsZero() {
		return c, fmt.Errorf("invalid cursor")
	}
	if _, err := uuid.Parse(c.ID); err != nil {
		return c, fmt.Errorf("invalid cursor")
	}
	return c, nil
}

type MessagePage struct {
	Items      []UserMessage `json:"messages"`
	Unread     int           `json:"unread"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor"`
}

func (s *Store) UserMessagePage(ctx context.Context, userID string, c CommunityCursor, size int, unreadOnly bool) (MessagePage, error) {
	if size < 1 || size > 100 {
		size = 20
	}
	p := MessagePage{Items: []UserMessage{}}
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,kind,event,data,read_at,created_at FROM user_messages WHERE user_id=$1 AND expires_at>now() AND (NOT $5::boolean OR read_at IS NULL) AND ($2::timestamptz IS NULL OR (created_at,id)<($2,$3::uuid)) ORDER BY created_at DESC,id DESC LIMIT $4`, userID, cursorTime(c), cursorID(c), size+1, unreadOnly)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var item UserMessage
		if err := rows.Scan(&item.ID, &item.Kind, &item.Event, &item.Data, &item.ReadAt, &item.CreatedAt); err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}
	rows.Close()
	p.HasMore = len(p.Items) > size
	if p.HasMore {
		p.Items = p.Items[:size]
		last := p.Items[len(p.Items)-1]
		p.NextCursor = EncodeCommunityCursor(last.CreatedAt, last.ID)
	}
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND read_at IS NULL AND expires_at>now()`, userID).Scan(&p.Unread)
	return p, err
}
func cursorTime(c CommunityCursor) any {
	if c.At.IsZero() {
		return nil
	}
	return c.At
}
func cursorID(c CommunityCursor) any {
	if c.ID == "" {
		return nil
	}
	return c.ID
}

type CommentPage struct {
	Items      []Comment `json:"comments"`
	HasMore    bool      `json:"has_more"`
	NextCursor string    `json:"next_cursor"`
}
type CommentThread struct {
	Root           Comment   `json:"root"`
	Replies        []Comment `json:"replies"`
	ResourceID     string    `json:"resource_id"`
	NextCursor     string    `json:"next_cursor"`
	PreviousCursor string    `json:"previous_cursor"`
}

var threadSelect = strings.Replace(commentSelect, "\nFROM", `,COALESCE(comment.reply_to_id::text,''),COALESCE(target_author.username,''),(SELECT count(*) FROM resource_comments replies WHERE replies.parent_id=comment.id AND replies.moderation_state='visible' AND replies.deleted_at IS NULL)
FROM`, 1) + ` LEFT JOIN resource_comments target ON target.id=comment.reply_to_id LEFT JOIN users target_author ON target_author.id=target.user_id `

func scanThreadComment(scanner interface{ Scan(...any) error }, c *Comment) error {
	var edited sql.NullTime
	err := scanner.Scan(&c.ID, &c.ResourceID, &c.UserID, &c.ParentID, &c.Body, &c.ModerationState, &c.Deleted, &c.Username, &c.AvatarURL, &c.BandBBSUserID, &c.CreatedAt, &edited, &c.ReplyToID, &c.ReplyToUsername, &c.ReplyCount)
	if edited.Valid {
		c.EditedAt = &edited.Time
	}
	if c.Deleted {
		c.Body = ""
	}
	return err
}
func (s *Store) readThreadComments(ctx context.Context, q string, args ...any) ([]Comment, error) {
	rows, err := s.db.QueryContext(ctx, threadSelect+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Comment{}
	for rows.Next() {
		var c Comment
		if err := scanThreadComment(rows, &c); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}
func (s *Store) CommentRoots(ctx context.Context, resource string, c CommunityCursor, size int) (CommentPage, error) {
	if size < 1 || size > 50 {
		size = 20
	}
	p := CommentPage{}
	items, err := s.readThreadComments(ctx, `WHERE comment.resource_id=$1 AND comment.parent_id IS NULL AND comment.moderation_state='visible' AND (comment.deleted_at IS NULL OR EXISTS(SELECT 1 FROM resource_comments r WHERE r.parent_id=comment.id AND r.moderation_state='visible' AND r.deleted_at IS NULL)) AND ($2::timestamptz IS NULL OR (comment.created_at,comment.id)<($2,$3::uuid)) ORDER BY comment.created_at DESC,comment.id DESC LIMIT $4`, resource, cursorTime(c), cursorID(c), size+1)
	if err != nil {
		return p, err
	}
	p.HasMore = len(items) > size
	if p.HasMore {
		items = items[:size]
		last := items[len(items)-1]
		p.NextCursor = EncodeCommunityCursor(last.CreatedAt, last.ID)
	}
	for i := range items {
		items[i].Replies, err = s.readThreadComments(ctx, `WHERE comment.parent_id=$1 AND comment.moderation_state='visible' AND comment.deleted_at IS NULL ORDER BY comment.created_at,comment.id LIMIT 3`, items[i].ID)
		if err != nil {
			return p, err
		}
	}
	p.Items = items
	return p, nil
}
func (s *Store) CommentThread(ctx context.Context, id string, before, after CommunityCursor, size int) (CommentThread, error) {
	var p CommentThread
	p.Replies = []Comment{}
	if size < 1 || size > 50 {
		size = 20
	}
	target, err := s.readThreadComments(ctx, `JOIN resources resource ON resource.id=comment.resource_id WHERE comment.id=$1 AND comment.moderation_state='visible' AND resource.moderation_state='visible' AND resource.current_revision_id IS NOT NULL`, id)
	if err != nil {
		return p, err
	}
	if len(target) == 0 {
		return p, ErrCommentNotFound
	}
	t := target[0]
	rootID := t.ParentID
	if rootID == "" {
		rootID = t.ID
	}
	roots, err := s.readThreadComments(ctx, `WHERE comment.id=$1 AND comment.moderation_state='visible'`, rootID)
	if err != nil {
		return p, err
	}
	if len(roots) == 0 {
		return p, ErrCommentNotFound
	}
	p.Root = roots[0]
	p.ResourceID = t.ResourceID
	if t.Deleted && t.ParentID != "" {
		return p, ErrCommentNotFound
	}
	q := `WHERE comment.parent_id=$1 AND comment.moderation_state='visible' AND comment.deleted_at IS NULL`
	args := []any{rootID}
	offset := 0
	reverse := false
	if !before.At.IsZero() {
		q += ` AND (comment.created_at,comment.id)<($2,$3::uuid)`
		args = append(args, before.At, before.ID)
		reverse = true
	} else if !after.At.IsZero() {
		q += ` AND (comment.created_at,comment.id)>($2,$3::uuid)`
		args = append(args, after.At, after.ID)
	} else if t.ParentID != "" {
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_comments WHERE parent_id=$1 AND moderation_state='visible' AND deleted_at IS NULL AND (created_at,id)<($2,$3::uuid)`, rootID, t.CreatedAt, t.ID).Scan(&offset); err != nil {
			return p, err
		}
		offset = (offset / size) * size
	}
	order := " ASC"
	if reverse {
		order = " DESC"
	}
	q += ` ORDER BY comment.created_at` + order + `,comment.id` + order + fmt.Sprintf(" LIMIT %d OFFSET %d", size, offset)
	replies, err := s.readThreadComments(ctx, q, args...)
	if err != nil {
		return p, err
	}
	if reverse {
		for i, j := 0, len(replies)-1; i < j; i, j = i+1, j-1 {
			replies[i], replies[j] = replies[j], replies[i]
		}
	}
	p.Replies = replies
	if len(replies) > 0 {
		first, last := replies[0], replies[len(replies)-1]
		var prev, next bool
		err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resource_comments WHERE parent_id=$1 AND moderation_state='visible' AND deleted_at IS NULL AND (created_at,id)<($2,$3::uuid)),EXISTS(SELECT 1 FROM resource_comments WHERE parent_id=$1 AND moderation_state='visible' AND deleted_at IS NULL AND (created_at,id)>($4,$5::uuid))`, rootID, first.CreatedAt, first.ID, last.CreatedAt, last.ID).Scan(&prev, &next)
		if err != nil {
			return p, err
		}
		if prev {
			p.PreviousCursor = EncodeCommunityCursor(first.CreatedAt, first.ID)
		}
		if next {
			p.NextCursor = EncodeCommunityCursor(last.CreatedAt, last.ID)
		}
	}
	return p, nil
}
