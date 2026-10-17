package schema

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateSchemaV2(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_revisions ADD COLUMN paid_type text NOT NULL DEFAULT 'free'`); err != nil {
		return fmt.Errorf("add resource revision paid type: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_revisions ADD CONSTRAINT resource_revisions_paid_type_check CHECK (paid_type IN ('free','paid','force_paid'))`); err != nil {
		return fmt.Errorf("validate resource revision paid type: %w", err)
	}
	steps := []func(context.Context, *sql.Tx) error{migrateRevisionProvenance, migrateDeviceAdministration, migrateGovernanceAndReleases, migratePluginVersions, migrateFeedbackHistory, migrateCollectionLifecycle, migratePublicationHistory, migrateStructuredAudit}
	for _, step := range steps {
		if err := step(ctx, tx); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(2)`); err != nil {
		return fmt.Errorf("record schema version 2: %w", err)
	}
	return nil
}
