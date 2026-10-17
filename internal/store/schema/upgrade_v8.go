package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV8(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, communitySchemaV8+communityNotificationsV8); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(8)`)
	return err
}
