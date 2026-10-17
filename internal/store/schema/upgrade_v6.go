package schema

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateSchemaV6(ctx context.Context, tx *sql.Tx) error {
	// Additive on purpose. Released OronBox clients read title and body, so
	// those stay and the structured pair is added beside them.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE user_messages ADD COLUMN event text NOT NULL DEFAULT '', ADD COLUMN data jsonb NOT NULL DEFAULT '{}'; CREATE INDEX user_messages_event_idx ON user_messages(event,created_at DESC) WHERE event<>''`); err != nil {
		return fmt.Errorf("add structured message fields: %w", err)
	}
	// Existing rows predate the taxonomy, so they get a namespaced marker
	// rather than a guessed event a client would then mis-route.
	if _, err := tx.ExecContext(ctx, `UPDATE user_messages SET event='legacy.'||kind WHERE event=''`); err != nil {
		return fmt.Errorf("backfill message events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE review_cases ADD COLUMN priority smallint NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 3), ADD COLUMN due_at timestamptz, ADD COLUMN first_submitted_at timestamptz`); err != nil {
		return fmt.Errorf("add review case scheduling: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE review_cases SET first_submitted_at=created_at WHERE first_submitted_at IS NULL; UPDATE review_cases SET due_at=created_at+interval '48 hours' WHERE due_at IS NULL`); err != nil {
		return fmt.Errorf("backfill review case scheduling: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE review_cases
 ALTER COLUMN first_submitted_at SET DEFAULT now(), ALTER COLUMN first_submitted_at SET NOT NULL,
 ALTER COLUMN due_at SET DEFAULT now()+interval '48 hours', ALTER COLUMN due_at SET NOT NULL`); err != nil {
		return fmt.Errorf("require review case scheduling: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX review_cases_queue_idx ON review_cases(state,priority DESC,due_at,created_at) WHERE state='pending'`); err != nil {
		return fmt.Errorf("index the review queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE comment_moderation ADD COLUMN human_reviewer_id uuid REFERENCES users(id) ON DELETE SET NULL, ADD COLUMN human_note text NOT NULL DEFAULT '', ADD COLUMN recheck_state text NOT NULL DEFAULT 'none' CHECK (recheck_state IN ('none','queued','in_review','done')), ADD COLUMN recheck_requested_at timestamptz, ADD COLUMN recheck_requested_by uuid REFERENCES users(id) ON DELETE SET NULL; CREATE INDEX comment_moderation_queue_idx ON comment_moderation(recheck_state,recheck_requested_at) WHERE recheck_state IN ('queued','in_review')`); err != nil {
		return fmt.Errorf("add comment recheck queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, governanceSchema); err != nil {
		return fmt.Errorf("create moderation tables: %w", err)
	}
	// Existing triggers keep their names and pick up the replaced bodies, so
	// only the genuinely new triggers are created here.
	if _, err := tx.ExecContext(ctx, notificationFunctions+notificationTriggersV6); err != nil {
		return fmt.Errorf("upgrade notification triggers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, reasonTemplateSeed); err != nil {
		return fmt.Errorf("seed moderation reason templates: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(6)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v6: %w", err)
	}
	return nil
}
