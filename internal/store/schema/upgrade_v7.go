package schema

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateSchemaV7(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, notificationFunctions); err != nil {
		return fmt.Errorf("refresh structured notification functions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE user_messages AS m
SET event=CASE WHEN m.event='' THEN 'legacy.'||m.kind ELSE m.event END,
    data=m.data||jsonb_build_object(
      'title',m.title,
      'body',m.body,
      'ref',m.ref,
      'resource_id',CASE
        WHEN m.kind IN ('comment_reply','moderation') THEN COALESCE(
          (SELECT comment.resource_id::text FROM resource_comments comment WHERE comment.id::text=m.ref),
          (SELECT resource.id::text FROM resources resource WHERE resource.id::text=m.ref),
          '')
        WHEN m.kind='review_result' THEN COALESCE(
          (SELECT revision.resource_id::text FROM resource_revisions revision WHERE revision.id::text=m.ref),
          '')
        WHEN m.kind='report_result' THEN COALESCE((SELECT CASE
          WHEN lower(ticket.target_source) IN ('oronbox','resource') THEN ticket.target_id
          WHEN lower(ticket.target_source)='comment' THEN COALESCE((SELECT comment.resource_id::text FROM resource_comments comment WHERE comment.id::text=ticket.target_id),'')
          ELSE '' END
          FROM feedback_tickets ticket WHERE ticket.id::text=m.ref),'')
        ELSE ''
      END,
      'comment_id',CASE
        WHEN m.kind IN ('comment_reply','moderation') AND EXISTS(SELECT 1 FROM resource_comments comment WHERE comment.id::text=m.ref) THEN m.ref
        WHEN m.kind='report_result' THEN COALESCE((SELECT CASE WHEN lower(ticket.target_source)='comment' THEN ticket.target_id ELSE '' END FROM feedback_tickets ticket WHERE ticket.id::text=m.ref),'')
        ELSE ''
      END
    )
WHERE m.event='' OR m.event LIKE 'legacy.%'`); err != nil {
		return fmt.Errorf("preserve legacy notification content as metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(7)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v7: %w", err)
	}
	return nil
}
