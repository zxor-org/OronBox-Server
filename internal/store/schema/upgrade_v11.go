package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV11(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, collectionsV11); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(11)`)
	return err
}
