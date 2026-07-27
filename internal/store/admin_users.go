package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AdminUserItem struct {
	ID              string
	BandBBSUserID   int64
	Username        string
	AvatarURL       string
	Role            string
	BannedAt        *time.Time
	BanReason       string
	CreatorFrozenAt *time.Time
	ResourceCount   int
	TicketCount     int
	CreatedAt       time.Time
}

type AdminUserQuery struct {
	Search  string
	Page    int
	PerPage int
}

func (query AdminUserQuery) normalized() AdminUserQuery {
	query.Search = strings.TrimSpace(query.Search)
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PerPage < 1 {
		query.PerPage = 25
	}
	if query.PerPage > 100 {
		query.PerPage = 100
	}
	return query
}

type AdminUserPage struct {
	Items      []AdminUserItem
	Total      int
	Page       int
	PerPage    int
	TotalPages int
	Query      AdminUserQuery
}

var ErrAdminUserNotFound = errors.New("user was not found")

func (s *Store) AdminUsers(ctx context.Context, raw AdminUserQuery) (AdminUserPage, error) {
	query := raw.normalized()
	filter := `($1='' OR u.username ILIKE '%'||$1||'%' OR CAST(u.bandbbs_user_id AS text)=$1)`
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users u WHERE `+filter, query.Search).Scan(&total); err != nil {
		return AdminUserPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT u.id::text,u.bandbbs_user_id,u.username,u.avatar_url,u.role,u.banned_at,u.ban_reason,u.creator_frozen_at,
 (SELECT count(*) FROM resources r WHERE r.owner_id=u.id),
 (SELECT count(*) FROM feedback_tickets t WHERE t.user_id=u.id),
 u.created_at
FROM users u WHERE `+filter+` ORDER BY u.created_at DESC LIMIT $2 OFFSET $3`,
		query.Search, query.PerPage, (query.Page-1)*query.PerPage)
	if err != nil {
		return AdminUserPage{}, err
	}
	defer rows.Close()
	page := AdminUserPage{Items: []AdminUserItem{}, Total: total, Page: query.Page, PerPage: query.PerPage, Query: query}
	for rows.Next() {
		var item AdminUserItem
		var bannedAt, frozenAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.BandBBSUserID, &item.Username, &item.AvatarURL, &item.Role, &bannedAt, &item.BanReason, &frozenAt, &item.ResourceCount, &item.TicketCount, &item.CreatedAt); err != nil {
			return AdminUserPage{}, err
		}
		if bannedAt.Valid {
			item.BannedAt = &bannedAt.Time
		}
		if frozenAt.Valid {
			item.CreatorFrozenAt = &frozenAt.Time
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AdminUserPage{}, err
	}
	page.TotalPages = (page.Total + page.PerPage - 1) / page.PerPage
	return page, nil
}

// AdminManageUser applies a governance action to a user: ban (which also
// revokes every client session), unban, freeze_creator, unfreeze_creator or
// set_role. Bans and freezes are annotated with the acting admin.
func (s *Store) AdminManageUser(ctx context.Context, id, action, reason, role string, actor AdminSession) (AdminUserItem, error) {
	if _, err := uuid.Parse(id); err != nil {
		return AdminUserItem{}, ErrAdminUserNotFound
	}
	reason = strings.TrimSpace(reason)
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AdminUserItem{}, err
	}
	defer tx.Rollback()
	var currentRole string
	err = tx.QueryRowContext(ctx, `SELECT role FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&currentRole)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminUserItem{}, ErrAdminUserNotFound
	}
	if err != nil {
		return AdminUserItem{}, err
	}
	if actor.UserID == id && action != "set_role" {
		return AdminUserItem{}, fmt.Errorf("%w: administrators cannot suspend their own account", ErrAdminResourceConflict)
	}
	switch action {
	case "ban":
		if _, err := tx.ExecContext(ctx, `UPDATE users SET banned_at=now(),ban_reason=$2,updated_at=now() WHERE id=$1`, id, reason); err != nil {
			return AdminUserItem{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, id); err != nil {
			return AdminUserItem{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM admin_sessions WHERE user_id=$1`, id); err != nil {
			return AdminUserItem{}, err
		}
	case "unban":
		if _, err := tx.ExecContext(ctx, `UPDATE users SET banned_at=NULL,ban_reason='',updated_at=now() WHERE id=$1`, id); err != nil {
			return AdminUserItem{}, err
		}
	case "freeze_creator":
		if _, err := tx.ExecContext(ctx, `UPDATE users SET creator_frozen_at=now(),updated_at=now() WHERE id=$1`, id); err != nil {
			return AdminUserItem{}, err
		}
	case "unfreeze_creator":
		if _, err := tx.ExecContext(ctx, `UPDATE users SET creator_frozen_at=NULL,updated_at=now() WHERE id=$1`, id); err != nil {
			return AdminUserItem{}, err
		}
	case "set_role":
		if role != "user" && role != "reviewer" && role != "admin" {
			return AdminUserItem{}, fmt.Errorf("%w: unknown role %q", ErrAdminResourceConflict, role)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET role=$2,updated_at=now() WHERE id=$1`, id, role); err != nil {
			return AdminUserItem{}, err
		}
	default:
		return AdminUserItem{}, fmt.Errorf("%w: unknown action", ErrAdminResourceConflict)
	}
	title, body := accountActionMessage(action, reason, role)
	if title != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_messages(id,user_id,kind,title,body,ref) VALUES(gen_random_uuid(),$1,'account',$2,$3,$4)`, id, title, body, action); err != nil {
			return AdminUserItem{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AdminUserItem{}, err
	}
	var item AdminUserItem
	row := s.db.QueryRowContext(ctx, `
SELECT u.id::text,u.bandbbs_user_id,u.username,u.avatar_url,u.role,u.banned_at,u.ban_reason,u.creator_frozen_at,
 (SELECT count(*) FROM resources r WHERE r.owner_id=u.id),
 (SELECT count(*) FROM feedback_tickets t WHERE t.user_id=u.id),
 u.created_at
FROM users u WHERE u.id=$1`, id)
	var bannedAt, frozenAt sql.NullTime
	if err := row.Scan(&item.ID, &item.BandBBSUserID, &item.Username, &item.AvatarURL, &item.Role, &bannedAt, &item.BanReason, &frozenAt, &item.ResourceCount, &item.TicketCount, &item.CreatedAt); err != nil {
		return AdminUserItem{}, err
	}
	if bannedAt.Valid {
		item.BannedAt = &bannedAt.Time
	}
	if frozenAt.Valid {
		item.CreatorFrozenAt = &frozenAt.Time
	}
	return item, err
}

func accountActionMessage(action, reason, role string) (string, string) {
	switch action {
	case "ban":
		body := "你的账号已被管理员封禁"
		if reason != "" {
			body += "：" + reason
		}
		return "账号已封禁", body
	case "unban":
		return "账号已解封", "你的账号已恢复正常使用"
	case "freeze_creator":
		return "创作者功能已冻结", "你暂时无法提交或管理资源"
	case "unfreeze_creator":
		return "创作者功能已恢复", "你现在可以继续提交和管理资源"
	case "set_role":
		return "账号角色已更新", "你的账号角色已调整为 " + role
	default:
		return "", ""
	}
}
