package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrMessageRecipients = errors.New("at least one valid recipient is required")

type UserMessage struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Event     string          `json:"event"`
	Data      json.RawMessage `json:"data"`
	ReadAt    *time.Time      `json:"read_at,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type Announcement struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
}

func (s *Store) UserMessages(ctx context.Context, userID string) ([]UserMessage, int, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT message.id::text,message.kind,message.event,message.data,message.read_at,message.created_at
FROM user_messages message WHERE message.user_id=$1 AND message.expires_at>now() ORDER BY message.created_at DESC LIMIT 100`, userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []UserMessage{}
	for rows.Next() {
		var item UserMessage
		if err := rows.Scan(&item.ID, &item.Kind, &item.Event, &item.Data, &item.ReadAt, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var unread int
	err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM user_messages WHERE user_id=$1 AND read_at IS NULL AND expires_at>now()`, userID).Scan(&unread)
	return items, unread, err
}

func (s *Store) ClearUserMessages(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_messages WHERE user_id=$1`, userID)
	return err
}

func (s *Store) ReadUserMessage(ctx context.Context, userID, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_messages SET read_at=COALESCE(read_at,now()) WHERE id=$1 AND user_id=$2`, id, userID)
	return err
}

func (s *Store) UnreadAnnouncements(ctx context.Context, userID string) ([]Announcement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT announcement.id::text,announcement.title,announcement.body,announcement.published_at FROM announcements announcement JOIN users ON users.id=$1 WHERE announcement.published_at>COALESCE(users.last_announcement_read_at,'-infinity') AND announcement.published_at>now()-interval '90 days' ORDER BY announcement.published_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Announcement{}
	for rows.Next() {
		var item Announcement
		if err := rows.Scan(&item.ID, &item.Title, &item.Body, &item.PublishedAt); err != nil {
			return nil, err
		}
		item.Type = "announcement"
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReadAnnouncements(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_announcement_read_at=now() WHERE id=$1`, userID)
	return err
}

func (s *Store) CreateAnnouncement(ctx context.Context, actorID, title, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := uuid.NewString()
	if _, err := tx.ExecContext(ctx, `INSERT INTO announcements(id,title,body,created_by) VALUES($1,$2,$3,$4)`, id, title, body, actorID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) SELECT gen_random_uuid(),id,'announcement','announcement.published',jsonb_build_object('announcement_id',$1::text,'title',$2::text,'body',$3::text),'','',$1 FROM users`, id, title, body); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AdminAnnouncements(ctx context.Context) ([]Announcement, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,title,body,published_at FROM announcements ORDER BY published_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Announcement{}
	for rows.Next() {
		var item Announcement
		if err := rows.Scan(&item.ID, &item.Title, &item.Body, &item.PublishedAt); err != nil {
			return nil, err
		}
		item.Type = "announcement"
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteAnnouncement(ctx context.Context, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return sql.ErrNoRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_messages WHERE kind='announcement' AND ref=$1`, id); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM announcements WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) CreateAdminMessages(ctx context.Context, userIDs []string, title, body string) (int64, error) {
	valid := make([]string, 0, len(userIDs))
	seen := map[string]bool{}
	for _, id := range userIDs {
		id = strings.TrimSpace(id)
		if _, err := uuid.Parse(id); err == nil && !seen[id] {
			seen[id] = true
			valid = append(valid, id)
		}
	}
	if len(valid) == 0 {
		return 0, ErrMessageRecipients
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO user_messages(id,user_id,kind,event,data,title,body) SELECT gen_random_uuid(),id,'admin_message','admin.message',jsonb_build_object('title',$2::text,'body',$3::text),'','' FROM users WHERE id=ANY($1::uuid[])`, valid, title, body)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
