package store_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/zxor-org/OronBox-Server/internal/store"
)

// TestMigrateV1ToV2 verifies the resource state machine unification: archived
// resources become owner takedowns, suspended resources are attributed to
// admins, and the legacy lifecycle column is dropped.
func TestMigrateV1ToV2(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := store.Open(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	databaseName := "testdb_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := db.ExecContext(ctx, `CREATE DATABASE `+databaseName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP DATABASE `+databaseName)
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + databaseName
	legacy, err := store.Open(parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	const v1 = `
CREATE TABLE users (id uuid PRIMARY KEY, bandbbs_user_id bigint NOT NULL UNIQUE, username text NOT NULL);
CREATE TABLE resources (
 id uuid PRIMARY KEY, owner_id uuid NOT NULL REFERENCES users(id), slug text NOT NULL,
 platform text NOT NULL DEFAULT 'vela_os', kind text NOT NULL,
 state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','archived')),
 moderation_state text NOT NULL DEFAULT 'visible' CHECK (moderation_state IN ('visible','suspended')),
 current_revision_id uuid, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX resources_governance_idx ON resources(moderation_state,state,updated_at DESC);
CREATE TABLE blobs (sha256 char(64) PRIMARY KEY);
CREATE TABLE resource_revisions (id uuid PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE);
CREATE TABLE revision_artifacts (id uuid PRIMARY KEY, revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE, blob_sha256 char(64) NOT NULL REFERENCES blobs(sha256));
INSERT INTO users VALUES ('11111111-1111-1111-1111-111111111111',1,'migration-user');
INSERT INTO resources(id,owner_id,slug,kind,state) VALUES ('22222222-2222-2222-2222-222222222222','11111111-1111-1111-1111-111111111111','archived-legacy','quickapp','archived');
INSERT INTO resources(id,owner_id,slug,kind,moderation_state) VALUES ('33333333-3333-3333-3333-333333333333','11111111-1111-1111-1111-111111111111','suspended-legacy','quickapp','suspended');
`
	if _, err := legacy.ExecContext(ctx, v1); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `CREATE TABLE schema_migrations (version bigint PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	states := map[string][2]string{}
	rows, err := legacy.QueryContext(ctx, `SELECT slug,moderation_state,COALESCE(moderation_by,'') FROM resources`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var slug, state, by string
		if err := rows.Scan(&slug, &state, &by); err != nil {
			t.Fatal(err)
		}
		states[slug] = [2]string{state, by}
	}
	if got := states["archived-legacy"]; got != [2]string{"suspended", "owner"} {
		t.Fatalf("archived-legacy migrated to %v, want [suspended owner]", got)
	}
	if got := states["suspended-legacy"]; got != [2]string{"suspended", "admin"} {
		t.Fatalf("suspended-legacy migrated to %v, want [suspended admin]", got)
	}
	var legacyColumn bool
	if err := legacy.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_name='resources' AND column_name='state')`).Scan(&legacyColumn); err != nil {
		t.Fatal(err)
	}
	if legacyColumn {
		t.Fatal("resources.state survived the v2 migration")
	}
}
