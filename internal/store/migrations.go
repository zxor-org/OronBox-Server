package store

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion int64 = 1

// Migrate creates and updates the database schema.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version bigint PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create schema version table: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v%d transaction: %w", schemaVersion, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	var installedVersion sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT max(version) FROM schema_migrations`).Scan(&installedVersion); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if installedVersion.Valid && installedVersion.Int64 == schemaVersion {
		return nil
	}
	if installedVersion.Valid {
		return fmt.Errorf("database schema version %d is unsupported; drop and recreate the database for schema version %d", installedVersion.Int64, schemaVersion)
	}
	var dirty bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name<>'schema_migrations')`).Scan(&dirty); err != nil {
		return fmt.Errorf("inspect database: %w", err)
	}
	if dirty {
		return fmt.Errorf("database is not empty; drop and recreate it before installing the OronBox schema")
	}
	const schema = `
CREATE TABLE users (
 id uuid PRIMARY KEY, bandbbs_user_id bigint NOT NULL UNIQUE, username text NOT NULL, avatar_url text NOT NULL DEFAULT '',
 role text NOT NULL DEFAULT 'user' CHECK (role IN ('user','reviewer','admin')),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE oauth_states (
 id text PRIMARY KEY, provider text NOT NULL, purpose text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL, used_at timestamptz, app_id text NOT NULL, app_version text NOT NULL,
 app_build text NOT NULL, platform text NOT NULL, return_uri text NOT NULL, user_id uuid REFERENCES users(id) ON DELETE CASCADE,
 ip inet, user_agent text NOT NULL DEFAULT '', secret_cipher bytea, completed_at timestamptz
);
CREATE INDEX oauth_states_expiry_idx ON oauth_states(expires_at);
CREATE TABLE oauth_events (
 id bigserial PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now(), provider text NOT NULL, event_type text NOT NULL,
 result text NOT NULL, app_id text NOT NULL DEFAULT '', app_version text NOT NULL DEFAULT '', app_build text NOT NULL DEFAULT '',
 platform text NOT NULL DEFAULT '', ip text NOT NULL DEFAULT '', user_agent text NOT NULL DEFAULT '', state_id text NOT NULL DEFAULT '',
 ticket_id text NOT NULL DEFAULT '', provider_user_id text NOT NULL DEFAULT '', expected_scopes text NOT NULL DEFAULT '',
 actual_scopes text NOT NULL DEFAULT '', error_code text NOT NULL DEFAULT '', error_message text NOT NULL DEFAULT '', latency_ms bigint NOT NULL DEFAULT 0
);
CREATE TABLE oauth_grants (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, provider text NOT NULL, subject text NOT NULL,
 scopes text[] NOT NULL DEFAULT '{}', access_token_cipher bytea NOT NULL, refresh_token_cipher bytea, token_type text NOT NULL DEFAULT 'Bearer',
 expires_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(user_id,provider)
);
CREATE TABLE login_tickets (
 id uuid PRIMARY KEY, ticket_hash bytea NOT NULL UNIQUE, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL, used_at timestamptz,
 app_id text NOT NULL, platform text NOT NULL, return_uri text NOT NULL, token_cipher bytea
);
CREATE TABLE sessions (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, access_hash bytea NOT NULL UNIQUE,
 refresh_hash bytea NOT NULL UNIQUE, access_expires_at timestamptz NOT NULL, refresh_expires_at timestamptz NOT NULL,
 revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), last_seen_at timestamptz NOT NULL DEFAULT now(),
 app_id text NOT NULL, app_version text NOT NULL, platform text NOT NULL, ip inet, user_agent text NOT NULL DEFAULT ''
);
CREATE INDEX sessions_user_idx ON sessions(user_id);
CREATE TABLE admin_sessions (
 id text PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, username text NOT NULL,
 ip inet, user_agent text NOT NULL DEFAULT ''
);
CREATE INDEX admin_sessions_expires_idx ON admin_sessions(expires_at);
CREATE TABLE devices (
 id uuid PRIMARY KEY, codename text NOT NULL UNIQUE, display_name text NOT NULL,
 platform text NOT NULL CHECK (platform IN ('vela_os','zepp_os')),
 astrobox_id text NOT NULL DEFAULT '', vendor text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE blobs (
 sha256 char(64) PRIMARY KEY, size_bytes bigint NOT NULL CHECK (size_bytes >= 0), media_type text NOT NULL,
 local_key text NOT NULL UNIQUE, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE blob_replicas (
 blob_sha256 char(64) NOT NULL REFERENCES blobs(sha256) ON DELETE CASCADE,
 backend text NOT NULL, object_key text NOT NULL DEFAULT '',
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','uploading','ready','failed')),
 error_message text NOT NULL DEFAULT '', attempts integer NOT NULL DEFAULT 0,
 next_attempt_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(blob_sha256,backend)
);
CREATE TABLE resources (
 id uuid PRIMARY KEY, owner_id uuid NOT NULL REFERENCES users(id), slug text NOT NULL,
 platform text NOT NULL DEFAULT 'vela_os' CHECK (platform='vela_os'),
 kind text NOT NULL CHECK (kind IN ('quickapp','watchface')),
 state text NOT NULL DEFAULT 'active' CHECK (state IN ('active','archived')),
 moderation_state text NOT NULL DEFAULT 'visible' CHECK (moderation_state IN ('visible','suspended')),
 current_revision_id uuid, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(owner_id,slug)
);
CREATE INDEX resources_governance_idx ON resources(moderation_state,state,updated_at DESC);
CREATE TABLE resource_revisions (
 id uuid PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 revision_no integer NOT NULL, name text NOT NULL, summary text NOT NULL,
 state text NOT NULL DEFAULT 'submitted' CHECK (state IN ('submitted','approved','rejected','superseded')),
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(resource_id,revision_no)
);
CREATE INDEX resource_revisions_resource_state_idx ON resource_revisions(resource_id,state,revision_no DESC);
ALTER TABLE resources ADD CONSTRAINT resources_current_revision_fk FOREIGN KEY (current_revision_id) REFERENCES resource_revisions(id);
CREATE TABLE revision_media (
 id uuid PRIMARY KEY, revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 blob_sha256 char(64) NOT NULL REFERENCES blobs(sha256), role text NOT NULL CHECK (role IN ('preview','icon','cover')),
 position integer NOT NULL DEFAULT 0, width integer NOT NULL, height integer NOT NULL, UNIQUE(revision_id,role,position)
);
CREATE TABLE revision_artifacts (
 id uuid PRIMARY KEY, revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 blob_sha256 char(64) NOT NULL REFERENCES blobs(sha256), original_name text NOT NULL,
 package_format text NOT NULL, package_id text NOT NULL DEFAULT '', package_version text NOT NULL DEFAULT '',
 analysis jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE revision_artifact_devices (
 revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 artifact_id uuid NOT NULL REFERENCES revision_artifacts(id) ON DELETE CASCADE,
 device_id uuid NOT NULL REFERENCES devices(id), PRIMARY KEY(artifact_id,device_id), UNIQUE(revision_id,device_id)
);
CREATE TABLE review_cases (
 id uuid PRIMARY KEY, revision_id uuid NOT NULL UNIQUE REFERENCES resource_revisions(id) ON DELETE CASCADE,
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','approved','rejected','superseded')),
 reviewer_id uuid REFERENCES users(id) ON DELETE SET NULL, note text NOT NULL DEFAULT '', items jsonb NOT NULL DEFAULT '[]',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE publications (
 id uuid PRIMARY KEY, revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 target text NOT NULL CHECK (target IN ('oronbox','bandbbs','astrobox')),
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','published','reviewing','failed','cancelled')),
 config jsonb NOT NULL DEFAULT '{}', external_id text NOT NULL DEFAULT '', external_url text NOT NULL DEFAULT '',
 error_message text NOT NULL DEFAULT '', status_detail jsonb NOT NULL DEFAULT '{}',
 attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(revision_id,target)
);
CREATE INDEX publications_dispatch_idx ON publications(state,target,next_attempt_at);
CREATE TABLE external_bindings (
 id uuid PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 provider text NOT NULL CHECK (provider IN ('bandbbs','astrobox')), external_id text NOT NULL,
 external_url text NOT NULL DEFAULT '', meta jsonb NOT NULL DEFAULT '{}',
 origin text NOT NULL DEFAULT 'published' CHECK (origin IN ('published','imported')),
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(provider,external_id), UNIQUE(resource_id,provider)
);
CREATE TABLE resource_events (
 id bigserial PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 actor_id uuid REFERENCES users(id) ON DELETE SET NULL, event_type text NOT NULL, payload jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE github_grants (
 user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE, github_user_id bigint NOT NULL, login text NOT NULL,
 access_token_cipher bytea NOT NULL, scopes text[] NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE github_device_flows (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, device_code_cipher bytea NOT NULL,
 user_code text NOT NULL, verification_uri text NOT NULL, interval_seconds integer NOT NULL, expires_at timestamptz NOT NULL,
 completed_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE audit_logs (
 id bigserial PRIMARY KEY, created_at timestamptz NOT NULL DEFAULT now(), actor_user_id uuid REFERENCES users(id) ON DELETE SET NULL,
 action text NOT NULL, result text NOT NULL, ip inet, user_agent text NOT NULL DEFAULT '', metadata jsonb NOT NULL DEFAULT '{}'
);
CREATE TABLE feedback_tickets (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('feedback','report')), subject text NOT NULL, message text NOT NULL,
 target_source text NOT NULL DEFAULT '', target_id text NOT NULL DEFAULT '', target_url text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','investigating','replied','resolved','dismissed','closed')),
 resolution text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), closed_at timestamptz
);
CREATE INDEX feedback_tickets_user_idx ON feedback_tickets(user_id,updated_at DESC);
CREATE INDEX feedback_tickets_status_idx ON feedback_tickets(status,updated_at DESC);
CREATE INDEX feedback_tickets_admin_idx ON feedback_tickets(kind,status,target_source,updated_at DESC);
CREATE INDEX feedback_tickets_target_idx ON feedback_tickets(target_source,target_id) WHERE kind='report';
CREATE TABLE feedback_replies (
 id uuid PRIMARY KEY, ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
 author_id uuid REFERENCES users(id) ON DELETE SET NULL, message text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_replies_ticket_idx ON feedback_replies(ticket_id,created_at);
`
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v%d: %w", schemaVersion, err)
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
