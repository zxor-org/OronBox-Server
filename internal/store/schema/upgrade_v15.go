package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV15(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, credentialsV15); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(15)`)
	return err
}
