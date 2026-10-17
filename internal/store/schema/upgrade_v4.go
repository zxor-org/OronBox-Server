package schema

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateSchemaV4(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_revisions ADD COLUMN purchase_link text NOT NULL DEFAULT '', ADD COLUMN purchase_price numeric(12,2), ADD COLUMN purchase_currency text NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add external purchase metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(4)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v4: %w", err)
	}
	return nil
}
