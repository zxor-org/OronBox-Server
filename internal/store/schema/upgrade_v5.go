package schema

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateSchemaV5(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resources DROP CONSTRAINT resources_moderation_state_check; ALTER TABLE resources ADD CONSTRAINT resources_moderation_state_check CHECK (moderation_state IN ('visible','suspended','frozen','deleted'))`); err != nil {
		return fmt.Errorf("allow deleted resource state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(5)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v5: %w", err)
	}
	return nil
}
