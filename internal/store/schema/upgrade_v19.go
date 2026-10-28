package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV19(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, reviewNotificationFunctions); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(19)`)
	return err
}
