package schema

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion int64 = 19

// Migrate installs the current schema into an empty database.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v%d transaction: %w", schemaVersion, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version bigint PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}
	var installedVersion sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&installedVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if installedVersion.Valid {
		if installedVersion.Int64 == schemaVersion {
			return nil
		}
		if installedVersion.Int64 >= 1 && installedVersion.Int64 < schemaVersion {
			steps := []struct {
				from  int64
				apply func(context.Context, *sql.Tx) error
			}{
				{from: 1, apply: migrateSchemaV2},
				{from: 2, apply: migrateSchemaV3},
				{from: 3, apply: migrateSchemaV4},
				{from: 4, apply: migrateSchemaV5},
				{from: 5, apply: migrateSchemaV6},
				{from: 6, apply: migrateSchemaV7},
				{from: 7, apply: migrateSchemaV8},
				{from: 8, apply: migrateSchemaV9},
				{from: 9, apply: migrateSchemaV10},
				{from: 10, apply: migrateSchemaV11},
				{from: 11, apply: migrateSchemaV12},
				{from: 12, apply: migrateSchemaV13},
				{from: 13, apply: migrateSchemaV14},
				{from: 14, apply: migrateSchemaV15},
				{from: 15, apply: migrateSchemaV16},
				{from: 16, apply: migrateSchemaV17},
				{from: 17, apply: migrateSchemaV18},
				{from: 18, apply: migrateSchemaV19},
			}
			for _, step := range steps {
				if installedVersion.Int64 > step.from {
					continue
				}
				if err := step.apply(ctx, tx); err != nil {
					return fmt.Errorf("apply PostgreSQL schema v%d: %w", step.from+1, err)
				}
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit PostgreSQL schema v%d: %w", schemaVersion, err)
			}
			return nil
		}
		return fmt.Errorf("database schema version %d is unsupported; drop and recreate the database for schema version %d", installedVersion.Int64, schemaVersion)
	}
	var dirty bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name<>'schema_migrations')`).Scan(&dirty); err != nil {
		return fmt.Errorf("inspect database: %w", err)
	}
	if dirty {
		return fmt.Errorf("database is not empty; drop and recreate it before installing the OronBox schema")
	}

	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v%d: %w", schemaVersion, err)
	}
	if _, err := tx.ExecContext(ctx, governanceSchema); err != nil {
		return fmt.Errorf("install PostgreSQL moderation tables: %w", err)
	}
	if _, err := tx.ExecContext(ctx, communitySchemaV8); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, resourceContentV9+bindingIdentitiesV9+deletionV10+collectionsV11+releasesV12+blogV13+bindingClaimsV14+credentialsV15+activityV16+consoleAuthV17); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, notificationSchema); err != nil {
		return fmt.Errorf("install PostgreSQL notification triggers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, reasonTemplateSeed); err != nil {
		return fmt.Errorf("seed moderation reason templates: %w", err)
	}
	if err := seedDevices(ctx, tx); err != nil {
		return fmt.Errorf("seed supported devices: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, schemaVersion); err != nil {
		return fmt.Errorf("record PostgreSQL schema v%d: %w", schemaVersion, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v%d: %w", schemaVersion, err)
	}
	return nil
}
