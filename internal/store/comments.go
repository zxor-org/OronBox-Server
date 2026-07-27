package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCommentNotFound = errors.New("comment was not found")
	ErrCommentTooFast  = errors.New("comments are limited to one every 5 seconds")
	ErrCommentQuota    = errors.New("daily comment quota exceeded")
)

type Comment struct {
	ID              string     `json:"id"`
	ResourceID      string     `json:"resource_id"`
	UserID          string     `json:"user_id"`
	ParentID        string     `json:"parent_id,omitempty"`
	Body            string     `json:"body"`
	Deleted         bool       `json:"deleted"`
	Username        string     `json:"username"`
	AvatarURL       string     `json:"avatar_url"`
	BandBBSUserID   int64      `json:"bandbbs_user_id"`
	ModerationState string     `json:"moderation_state,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	EditedAt        *time.Time `json:"edited_at,omitempty"`
	Replies         []Comment  `json:"replies,omitempty"`
}

const commentSelect = `
SELECT comment.id::text,comment.resource_id::text,comment.user_id::text,COALESCE(comment.parent_id::text,''),comment.body,
 comment.moderation_state,comment.deleted_at IS NOT NULL,author.username,author.avatar_url,author.bandbbs_user_id,comment.created_at,comment.edited_at
FROM resource_comments comment JOIN users author ON author.id=comment.user_id`

func scanComment(scanner interface{ Scan(dest ...any) error }, comment *Comment) error {
	var editedAt sql.NullTime
	err := scanner.Scan(&comment.ID, &comment.ResourceID, &comment.UserID, &comment.ParentID, &comment.Body,
		&comment.ModerationState, &comment.Deleted, &comment.Username, &comment.AvatarURL, &comment.BandBBSUserID, &comment.CreatedAt, &editedAt)
	if editedAt.Valid {
		comment.EditedAt = &editedAt.Time
	}
	return err
}

// ListComments returns one page of top-level comments for a resource with
// their one-level replies. Deleted comments stay as placeholders so reply
// threads keep their structure; hidden (pre-moderation) comments are only
// visible to their author.
func (s *Store) ListComments(ctx context.Context, resourceID, viewerID string, before time.Time, limit int) ([]Comment, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, commentSelect+`
WHERE comment.resource_id=$1 AND comment.parent_id IS NULL
 AND (comment.moderation_state='visible' OR comment.user_id=$2)
 AND comment.created_at<$3
ORDER BY comment.created_at DESC LIMIT $4`, resourceID, viewerID, before, limit)
	if err != nil {
		return nil, err
	}
	var comments []Comment
	ids := []string{}
	for rows.Next() {
		var comment Comment
		if err := scanComment(rows, &comment); err != nil {
			rows.Close()
			return nil, err
		}
		comments = append(comments, comment)
		ids = append(ids, comment.ID)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		replyRows, err := s.db.QueryContext(ctx, commentSelect+`
WHERE comment.parent_id=ANY($1) AND (comment.moderation_state='visible' OR comment.user_id=$2)
ORDER BY comment.created_at`, ids, viewerID)
		if err != nil {
			return nil, err
		}
		defer replyRows.Close()
		byParent := map[string][]Comment{}
		for replyRows.Next() {
			var reply Comment
			if err := scanComment(replyRows, &reply); err != nil {
				return nil, err
			}
			byParent[reply.ParentID] = append(byParent[reply.ParentID], reply)
		}
		for index := range comments {
			comments[index].Replies = byParent[comments[index].ID]
		}
	}
	return comments, nil
}

func (s *Store) CreateComment(ctx context.Context, resourceID, userID, parentID, body, moderationState string) (Comment, error) {
	return s.CreateModeratedComment(ctx, resourceID, userID, parentID, body, moderationState, nil)
}

type CommentModerationRecord struct {
	Provider, Model, Action, Reason string
	Categories                      []string
	Raw                             map[string]any
}

func (s *Store) CreateModeratedComment(ctx context.Context, resourceID, userID, parentID, body, moderationState string, moderation *CommentModerationRecord) (Comment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "comment:"+userID); err != nil {
		return Comment{}, err
	}
	var lastAt sql.NullTime
	var daily int
	err = tx.QueryRowContext(ctx, `SELECT max(created_at),count(*) FILTER (WHERE created_at>now()-interval '24 hours') FROM resource_comments WHERE user_id=$1`, userID).Scan(&lastAt, &daily)
	if err != nil {
		return Comment{}, err
	}
	if lastAt.Valid && time.Since(lastAt.Time) < 5*time.Second {
		return Comment{}, ErrCommentTooFast
	}
	if daily >= 20 {
		return Comment{}, ErrCommentQuota
	}
	var resourceVisible bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resources WHERE id=$1 AND moderation_state='visible' AND current_revision_id IS NOT NULL)`, resourceID).Scan(&resourceVisible); err != nil {
		return Comment{}, err
	}
	if !resourceVisible {
		return Comment{}, ErrCommentNotFound
	}
	if parentID != "" {
		var parentOK bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resource_comments WHERE id=$1 AND resource_id=$2 AND parent_id IS NULL AND moderation_state='visible' AND deleted_at IS NULL)`, parentID, resourceID).Scan(&parentOK); err != nil {
			return Comment{}, err
		}
		if !parentOK {
			return Comment{}, ErrCommentNotFound
		}
	}
	var comment Comment
	id := uuid.NewString()
	row := tx.QueryRowContext(ctx, `
INSERT INTO resource_comments(id,resource_id,user_id,parent_id,body,moderation_state)
VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6)
RETURNING id::text,resource_id::text,user_id::text,COALESCE(parent_id::text,''),body,moderation_state,false,
 (SELECT username FROM users WHERE id=$3),(SELECT avatar_url FROM users WHERE id=$3),(SELECT bandbbs_user_id FROM users WHERE id=$3),now(),NULL::timestamptz`,
		id, resourceID, userID, parentID, body, moderationState)
	if err := scanComment(row, &comment); err != nil {
		return Comment{}, err
	}
	if moderation != nil {
		if moderation.Categories == nil {
			moderation.Categories = []string{}
		}
		rawJSON, err := json.Marshal(moderation.Raw)
		if err != nil {
			return Comment{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO comment_moderation(comment_id,provider,model,action,categories,reason,raw_response) VALUES($1,$2,$3,$4,$5,$6,$7)`, comment.ID, moderation.Provider, moderation.Model, moderation.Action, moderation.Categories, moderation.Reason, rawJSON); err != nil {
			return Comment{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Comment{}, err
	}
	return comment, nil
}

// SoftDeleteComment marks a comment as deleted by its author.
func (s *Store) SoftDeleteComment(ctx context.Context, commentID, userID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE resource_comments SET deleted_at=now() WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL`, commentID, userID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrCommentNotFound
	}
	return nil
}

func (s *Store) CommentTargetExists(ctx context.Context, commentID string) (bool, error) {
	if _, err := uuid.Parse(commentID); err != nil {
		return false, nil
	}
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM resource_comments WHERE id=$1)`, commentID).Scan(&exists)
	return exists, err
}

type AdminCommentItem struct {
	Comment
	ModerationAction string
	ModerationReason string
	ModerationModel  string
	HumanReviewed    bool
}

func (s *Store) AdminCommentQueue(ctx context.Context, page, perPage int) ([]AdminCommentItem, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 25
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM comment_moderation moderation JOIN resource_comments comment ON comment.id=moderation.comment_id WHERE moderation.human_reviewed_at IS NULL AND moderation.action IN ('review','block')`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT comment.id::text,comment.resource_id::text,comment.user_id::text,COALESCE(comment.parent_id::text,''),comment.body,
 comment.moderation_state,comment.deleted_at IS NOT NULL,author.username,author.avatar_url,author.bandbbs_user_id,comment.created_at,comment.edited_at,
 moderation.action,moderation.reason,moderation.model
FROM resource_comments comment JOIN users author ON author.id=comment.user_id
JOIN comment_moderation moderation ON moderation.comment_id=comment.id
WHERE moderation.human_reviewed_at IS NULL AND moderation.action IN ('review','block')
ORDER BY comment.created_at LIMIT $1 OFFSET $2`, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []AdminCommentItem{}
	for rows.Next() {
		var item AdminCommentItem
		var editedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.ResourceID, &item.UserID, &item.ParentID, &item.Body, &item.ModerationState, &item.Deleted, &item.Username, &item.AvatarURL, &item.BandBBSUserID, &item.CreatedAt, &editedAt, &item.ModerationAction, &item.ModerationReason, &item.ModerationModel); err != nil {
			return nil, 0, err
		}
		if editedAt.Valid {
			item.EditedAt = &editedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// AdminModerateComment applies a human decision: "approve" makes the comment
// visible, "hide" keeps or makes it hidden. Either way the queue entry is
// marked as human-reviewed.
func (s *Store) AdminModerateComment(ctx context.Context, commentID, action string) error {
	if _, err := uuid.Parse(commentID); err != nil {
		return ErrCommentNotFound
	}
	state := "visible"
	if action == "hide" {
		state = "hidden"
	} else if action != "approve" {
		return ErrCommentNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE resource_comments SET moderation_state=$2 WHERE id=$1`, commentID, state)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrCommentNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE comment_moderation SET human_reviewed_at=now(),human_action=$2 WHERE comment_id=$1`, commentID, action); err != nil {
		return err
	}
	return tx.Commit()
}

// Setting reads a server setting, returning fallback when unset.
func (s *Store) Setting(ctx context.Context, key, fallback string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM server_settings WHERE key=$1`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	return value, err
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO server_settings(key,value,updated_at) VALUES($1,$2,now()) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=now()`, key, value)
	return err
}
