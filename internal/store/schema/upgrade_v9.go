package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV9(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, resourceContentV9+bindingIdentitiesV9); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(9)`)
	return err
}
