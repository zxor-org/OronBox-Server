package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

func inferLegacyAuditData(action, message string) AuditData {
	values := make(map[string]string)
	for _, field := range strings.Fields(message) {
		key, value, found := strings.Cut(field, "=")
		if found && key != "" && value != "" {
			values[key] = value
		}
	}
	data := AuditData{}
	if value := values["previous_moderation"]; value != "" {
		data.Before = map[string]any{"moderation_state": value}
	}
	if value := values["moderation"]; value != "" {
		data.After = map[string]any{"moderation_state": value}
	}
	for _, candidate := range []struct{ key, kind string }{
		{"resource", "resource"}, {"user_id", "user"}, {"user", "user"}, {"ticket", "ticket"},
		{"comment", "comment"}, {"revision", "revision"}, {"sha256", "blob"},
		{"plugin", "plugin"}, {"collection", "collection"}, {"publication", "publication"},
	} {
		if id := strings.TrimSpace(values[candidate.key]); id != "" {
			data.Target = AuditTarget{Type: candidate.kind, ID: strings.Trim(id, ",")}
			break
		}
	}
	if data.Target.Type == "ticket" && values["ticket_kind"] == "feedback" {
		data.Target.Type = "feedback"
	}
	return data
}

func (target AuditTarget) AdminURL() string {
	switch target.Type {
	case "resource":
		return "/admin/resources/" + target.ID
	case "user":
		return "/admin/users/" + target.ID
	case "ticket":
		return "/admin/reports/" + target.ID
	case "feedback":
		return "/admin/feedback/" + target.ID
	case "blob":
		return "/admin/storage/blobs/" + target.ID
	case "plugin":
		return "/admin/plugins/" + target.ID
	case "collection":
		return "/admin/collections/" + target.ID
	case "publication":
		return "/admin/publications/" + target.ID
	default:
		return ""
	}
}

const adminAuditSelect = `SELECT audit.id,audit.created_at,COALESCE(audit.actor_user_id::text,''),COALESCE(NULLIF(actor.username,''),audit.metadata->>'username',''),audit.action,audit.result,COALESCE(audit.ip::text,''),audit.user_agent,COALESCE(audit.metadata->>'message',''),audit.metadata,COALESCE(audit.before_data,'null'::jsonb),COALESCE(audit.after_data,'null'::jsonb),audit.target_data FROM audit_logs audit LEFT JOIN users actor ON actor.id=audit.actor_user_id`

func scanAuditLog(scanner interface{ Scan(...any) error }) (AuditLog, error) {
	var item AuditLog
	var created time.Time
	var metadataJSON, beforeJSON, afterJSON, targetJSON []byte
	if err := scanner.Scan(&item.ID, &created, &item.ActorUserID, &item.Username, &item.Action, &item.Result, &item.IP, &item.UserAgent, &item.Message, &metadataJSON, &beforeJSON, &afterJSON, &targetJSON); err != nil {
		return AuditLog{}, err
	}
	item.CreatedAt = formatTime(created)
	_ = json.Unmarshal(metadataJSON, &item.Metadata)
	_ = json.Unmarshal(beforeJSON, &item.Before)
	_ = json.Unmarshal(afterJSON, &item.After)
	_ = json.Unmarshal(targetJSON, &item.Target)
	// Rows written before structured audit fields were introduced remain useful
	// without destructive backfills.
	if item.Target.ID == "" {
		legacy := inferLegacyAuditData(item.Action, item.Message)
		item.Target, item.Before, item.After = legacy.Target, legacy.Before, legacy.After
	}
	return item, nil
}

func (s *Store) AdminAuditLog(ctx context.Context, id int64) (AuditLog, error) {
	if id < 1 {
		return AuditLog{}, sql.ErrNoRows
	}
	return scanAuditLog(s.db.QueryRowContext(ctx, adminAuditSelect+` WHERE audit.id=$1`, id))
}

func (s *Store) AdminAuditLogsForExport(ctx context.Context, raw AdminAuditLogQuery) ([]AuditLog, error) {
	q := raw.normalized()
	args := adminAuditFilterArgs(q)
	rows, err := s.db.QueryContext(ctx, adminAuditSelect+` WHERE `+adminAuditFilter+` ORDER BY audit.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AuditLog, 0)
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
