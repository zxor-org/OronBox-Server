package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV10(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, deletionV10); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(10)`)
	return err
}
