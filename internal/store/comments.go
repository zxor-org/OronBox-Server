package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCommentNotFound = errors.New("comment was not found")
	ErrCommentTooFast  = errors.New("comments are limited to five per minute")
	ErrCommentQuota    = errors.New("daily comment quota exceeded")
)

type Comment struct {
	ID               string     `json:"id"`
	ResourceID       string     `json:"resource_id"`
	UserID           string     `json:"user_id"`
	ParentID         string     `json:"parent_id,omitempty"`
	Body             string     `json:"body"`
	Deleted          bool       `json:"deleted"`
	Username         string     `json:"username"`
	AvatarURL        string     `json:"avatar_url"`
	BandBBSUserID    int64      `json:"bandbbs_user_id"`
	ModerationState  string     `json:"moderation_state,omitempty"`
	ModerationAction string     `json:"moderation_action,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	EditedAt         *time.Time `json:"edited_at,omitempty"`
	Replies          []Comment  `json:"replies,omitempty"`
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

// ListComments returns visible, non-deleted top-level comments and their
// visible, non-deleted one-level replies. Hidden and deleted records remain in
// storage for moderation and audit purposes, but are never public content.
func (s *Store) ListComments(ctx context.Context, resourceID, _ string, before time.Time, limit int) ([]Comment, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, commentSelect+`
WHERE comment.resource_id=$1 AND comment.parent_id IS NULL
 AND comment.moderation_state='visible' AND comment.deleted_at IS NULL
 AND comment.created_at<$2
ORDER BY comment.created_at DESC LIMIT $3`, resourceID, before, limit)
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
WHERE comment.parent_id=ANY($1) AND comment.moderation_state='visible' AND comment.deleted_at IS NULL
ORDER BY comment.created_at`, ids)
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
	var minute, daily int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE created_at>now()-interval '1 minute'),count(*) FILTER (WHERE created_at>now()-interval '24 hours') FROM resource_comments WHERE user_id=$1`, userID).Scan(&minute, &daily)
	if err != nil {
		return Comment{}, err
	}
	if minute >= 5 {
		return Comment{}, ErrCommentTooFast
	}
	if daily >= 100 {
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

// AdminDeleteComment marks any visible comment as deleted.
func (s *Store) AdminDeleteComment(ctx context.Context, commentID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE resource_comments SET deleted_at=now() WHERE id=$1 AND deleted_at IS NULL`, commentID)
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

type AdminCommentQuery struct {
	Search, State, Resource, User, Sort string
	Page, PerPage                       int
}

type AdminCommentPage struct {
	Items                            []AdminCommentItem
	Total, Page, PerPage, TotalPages int
	Query                            AdminCommentQuery
}

func (q AdminCommentQuery) normalized() AdminCommentQuery {
	q.Search, q.State = strings.TrimSpace(q.Search), strings.ToLower(strings.TrimSpace(q.State))
	q.Resource, q.User = strings.TrimSpace(q.Resource), strings.TrimSpace(q.User)
	if q.State != "visible" && q.State != "hidden" && q.State != "deleted" && q.State != "review" {
		q.State = ""
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 {
		q.PerPage = 25
	}
	if q.PerPage > 100 {
		q.PerPage = 100
	}
	switch q.Sort {
	case "oldest", "username", "state":
	default:
		q.Sort = "newest"
	}
	return q
}

func adminCommentOrder(sort string) string {
	switch sort {
	case "oldest":
		return "comment.created_at ASC,comment.id ASC"
	case "username":
		return "author.username ASC,comment.created_at DESC"
	case "state":
		return "comment.moderation_state ASC,comment.created_at DESC"
	default:
		return "comment.created_at DESC,comment.id DESC"
	}
}

// AdminComments returns the complete moderation corpus, including deleted and
// already-reviewed comments. State=review narrows to the pending AI queue.
func (s *Store) AdminComments(ctx context.Context, raw AdminCommentQuery) (AdminCommentPage, error) {
	q := raw.normalized()
	const filter = `($1='' OR concat_ws(' ',comment.body,author.username,comment.id::text,comment.resource_id::text,comment.user_id::text) ILIKE '%'||$1||'%')
AND ($2='' OR ($2='deleted' AND comment.deleted_at IS NOT NULL) OR ($2 IN ('visible','hidden') AND comment.deleted_at IS NULL AND comment.moderation_state=$2) OR ($2='review' AND moderation.action='review' AND moderation.human_reviewed_at IS NULL))
AND ($3='' OR comment.resource_id::text=$3) AND ($4='' OR comment.user_id::text=$4 OR author.username ILIKE '%'||$4||'%')`
	page := AdminCommentPage{Items: []AdminCommentItem{}, Page: q.Page, PerPage: q.PerPage, Query: q}
	base := ` FROM resource_comments comment JOIN users author ON author.id=comment.user_id LEFT JOIN comment_moderation moderation ON moderation.comment_id=comment.id WHERE ` + filter
	if err := s.db.QueryRowContext(ctx, `SELECT count(*)`+base, q.Search, q.State, q.Resource, q.User).Scan(&page.Total); err != nil {
		return AdminCommentPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT comment.id::text,comment.resource_id::text,comment.user_id::text,COALESCE(comment.parent_id::text,''),comment.body,comment.moderation_state,comment.deleted_at IS NOT NULL,author.username,author.avatar_url,author.bandbbs_user_id,comment.created_at,comment.edited_at,COALESCE(moderation.action,''),COALESCE(moderation.reason,''),COALESCE(moderation.model,''),moderation.human_reviewed_at IS NOT NULL%s ORDER BY %s LIMIT $5 OFFSET $6`, base, adminCommentOrder(q.Sort)), q.Search, q.State, q.Resource, q.User, q.PerPage, (q.Page-1)*q.PerPage)
	if err != nil {
		return AdminCommentPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AdminCommentItem
		var edited sql.NullTime
		if err := rows.Scan(&item.ID, &item.ResourceID, &item.UserID, &item.ParentID, &item.Body, &item.ModerationState, &item.Deleted, &item.Username, &item.AvatarURL, &item.BandBBSUserID, &item.CreatedAt, &edited, &item.ModerationAction, &item.ModerationReason, &item.ModerationModel, &item.HumanReviewed); err != nil {
			return AdminCommentPage{}, err
		}
		if edited.Valid {
			item.EditedAt = &edited.Time
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminCommentPage{}, err
	}
	if page.Total > 0 {
		page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	}
	return page, nil
}

func (s *Store) AdminCommentQueue(ctx context.Context, page, perPage int) ([]AdminCommentItem, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 25
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM comment_moderation moderation JOIN resource_comments comment ON comment.id=moderation.comment_id WHERE moderation.human_reviewed_at IS NULL AND moderation.action='review'`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT comment.id::text,comment.resource_id::text,comment.user_id::text,COALESCE(comment.parent_id::text,''),comment.body,
 comment.moderation_state,comment.deleted_at IS NOT NULL,author.username,author.avatar_url,author.bandbbs_user_id,comment.created_at,comment.edited_at,
 moderation.action,moderation.reason,moderation.model
FROM resource_comments comment JOIN users author ON author.id=comment.user_id
JOIN comment_moderation moderation ON moderation.comment_id=comment.id
WHERE moderation.human_reviewed_at IS NULL AND moderation.action='review'
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

// commentModerationState maps a human decision onto the comment state, and
// reports whether the action is one this store accepts at all.
func commentModerationState(action string) (string, bool) {
	switch action {
	case "approve":
		return "visible", true
	case "hide":
		return "hidden", true
	default:
		return "", false
	}
}

// AdminModerateComment applies a human decision: "approve" makes the comment
// visible, "hide" keeps or makes it hidden. Either way the queue entry is
// marked as human-reviewed, and who decided plus why is recorded so a disputed
// removal can be traced back to a person rather than to "the system".
func (s *Store) AdminModerateComment(ctx context.Context, commentID, action, actorID, note string) error {
	return s.moderateComments(ctx, []string{commentID}, action, actorID, note)
}

// AdminModerateCommentsBatch applies one decision to many comments in a single
// transaction. Like the review queue, it is all-or-nothing: a batch that would
// partly fail changes nothing, so the operator never has to work out which
// half of a hundred-comment action went through.
func (s *Store) AdminModerateCommentsBatch(ctx context.Context, commentIDs []string, action, actorID, note string) error {
	if len(commentIDs) == 0 {
		return errors.New("没有选择任何评论")
	}
	if len(commentIDs) > 100 {
		return errors.New("一次最多处理 100 条评论")
	}
	return s.moderateComments(ctx, commentIDs, action, actorID, note)
}

func (s *Store) moderateComments(ctx context.Context, commentIDs []string, action, actorID, note string) error {
	state, ok := commentModerationState(action)
	if !ok {
		return ErrCommentNotFound
	}
	for _, id := range commentIDs {
		if _, err := uuid.Parse(id); err != nil {
			return ErrCommentNotFound
		}
	}
	var reviewer any
	if _, err := uuid.Parse(actorID); err == nil {
		reviewer = actorID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE resource_comments SET moderation_state=$2 WHERE id=ANY($1::uuid[])`, commentIDs, state)
	if err != nil {
		return err
	}
	// Anything short of a full match means at least one id was wrong, so the
	// whole batch is abandoned rather than silently applied to the rest.
	if rows, _ := result.RowsAffected(); rows != int64(len(commentIDs)) {
		return ErrCommentNotFound
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE comment_moderation SET human_reviewed_at=now(),human_action=$2,human_reviewer_id=$3,human_note=$4,recheck_state='done' WHERE comment_id=ANY($1::uuid[])`,
		commentIDs, action, reviewer, note); err != nil {
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
