package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV14(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, bindingClaimsV14); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(14)`)
	return err
}
