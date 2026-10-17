package schema

import (
	"context"
	"database/sql"
)

func migrateSchemaV12(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, releasesV12); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(12)`)
	return err
}
