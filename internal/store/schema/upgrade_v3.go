package schema

import (
	"context"
	"database/sql"
	"fmt"
)

func migrateSchemaV3(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resources ADD COLUMN published_at timestamptz; ALTER TABLE resource_collections ADD COLUMN published_at timestamptz`); err != nil {
		return fmt.Errorf("add public resource timestamps: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resources resource SET published_at=COALESCE((SELECT revision.created_at FROM resource_revisions revision WHERE revision.id=resource.current_revision_id),resource.created_at) WHERE resource.current_revision_id IS NOT NULL`); err != nil {
		return fmt.Errorf("backfill resource publication timestamps: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_collections collection SET published_at=COALESCE((SELECT revision.created_at FROM resource_collection_revisions revision WHERE revision.id=collection.current_revision_id),collection.created_at) WHERE collection.current_revision_id IS NOT NULL`); err != nil {
		return fmt.Errorf("backfill collection publication timestamps: %w", err)
	}
	// Older admin forms allowed inactive target fields to remain populated when
	// an editor changed the target type. Normalize those harmless leftovers
	// before installing the stricter checks below. Rows with a missing required
	// target or an unknown type are intentionally preserved for later cleanup
	// instead of being silently discarded.
	if _, err := tx.ExecContext(ctx, `
UPDATE home_banners SET blog_slug=NULL,link_url='' WHERE type='resource';
UPDATE home_banners SET resource_id=NULL,link_url='' WHERE type='blog';
UPDATE home_banners SET resource_id=NULL,blog_slug=NULL WHERE type='link';
UPDATE home_section_cards SET blog_slug=NULL WHERE type='resource';
UPDATE home_section_cards SET resource_id=NULL WHERE type='blog'`); err != nil {
		return fmt.Errorf("normalize homepage target fields: %w", err)
	}
	// NOT VALID keeps an old installation upgradeable even if an early admin
	// form left a malformed target behind. PostgreSQL still enforces the check
	// for every new or updated row; the existing rows can be cleaned and the
	// constraints validated later without blocking startup.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE home_banners ADD CONSTRAINT home_banners_target_consistency CHECK ((type='resource' AND resource_id IS NOT NULL AND blog_slug IS NULL AND link_url='') OR (type='blog' AND resource_id IS NULL AND blog_slug IS NOT NULL AND link_url='') OR (type='link' AND resource_id IS NULL AND blog_slug IS NULL AND link_url<>'')) NOT VALID; ALTER TABLE home_section_cards ADD CONSTRAINT home_section_cards_target_consistency CHECK ((type='resource' AND resource_id IS NOT NULL AND blog_slug IS NULL) OR (type='blog' AND resource_id IS NULL AND blog_slug IS NOT NULL)) NOT VALID`); err != nil {
		return fmt.Errorf("enforce homepage target fields: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX resources_publication_idx ON resources(moderation_state,published_at DESC) WHERE current_revision_id IS NOT NULL; CREATE INDEX resource_collections_publication_idx ON resource_collections(enabled,published_at DESC) WHERE current_revision_id IS NOT NULL`); err != nil {
		return fmt.Errorf("index public resource timestamps: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(3)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v3: %w", err)
	}
	return nil
}
