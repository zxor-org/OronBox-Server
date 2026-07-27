package store

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion int64 = 8

const notificationSchema = `
CREATE FUNCTION notify_comment_reply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.parent_id IS NOT NULL AND NEW.moderation_state='visible' THEN
  INSERT INTO user_messages(id,user_id,kind,title,body,ref)
  SELECT md5(random()::text||clock_timestamp()::text)::uuid,parent.user_id,'comment_reply','评论收到回复',NEW.body,NEW.id::text
  FROM resource_comments parent WHERE parent.id=NEW.parent_id AND parent.user_id<>NEW.user_id;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER resource_comments_reply_message AFTER INSERT ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_reply();
CREATE TRIGGER resource_comments_approved_reply_message AFTER UPDATE OF moderation_state ON resource_comments FOR EACH ROW WHEN (OLD.moderation_state='hidden' AND NEW.moderation_state='visible') EXECUTE FUNCTION notify_comment_reply();
CREATE FUNCTION notify_comment_hidden() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.moderation_state<>NEW.moderation_state AND NEW.moderation_state='hidden' THEN
  INSERT INTO user_messages(id,user_id,kind,title,body,ref) VALUES(md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'moderation','评论已被隐藏','你的评论经人工复审后已被隐藏',NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER resource_comments_hidden_message AFTER UPDATE OF moderation_state ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_hidden();
CREATE FUNCTION notify_review_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>NEW.state AND NEW.state IN ('approved','rejected') THEN
  INSERT INTO user_messages(id,user_id,kind,title,body,ref)
  SELECT md5(random()::text||clock_timestamp()::text)::uuid,resource.owner_id,'review_result',
   CASE NEW.state WHEN 'approved' THEN '资源审核已通过' ELSE '资源审核未通过' END,
   CASE NEW.state WHEN 'approved' THEN '你的资源版本已通过审核' ELSE '你的资源版本需要修改'||CASE WHEN NEW.note='' THEN '' ELSE '：'||NEW.note END END,
   NEW.revision_id::text
  FROM resource_revisions revision JOIN resources resource ON resource.id=revision.resource_id WHERE revision.id=NEW.revision_id;
 END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN IF to_regclass('review_cases') IS NOT NULL THEN EXECUTE 'CREATE TRIGGER review_cases_result_message AFTER UPDATE OF state ON review_cases FOR EACH ROW EXECUTE FUNCTION notify_review_result()'; END IF; END $$;
CREATE FUNCTION notify_resource_moderation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.moderation_state<>NEW.moderation_state AND (NEW.moderation_by='admin' OR OLD.moderation_by='admin') THEN
  INSERT INTO user_messages(id,user_id,kind,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.owner_id,'moderation',
   CASE NEW.moderation_state WHEN 'suspended' THEN '资源已下架' WHEN 'frozen' THEN '资源已冻结' ELSE '资源已恢复' END,
   CASE NEW.moderation_state WHEN 'suspended' THEN '资源已下架' WHEN 'frozen' THEN '资源已冻结' ELSE '资源已恢复' END||CASE WHEN COALESCE(NULLIF(NEW.moderation_reason,''),OLD.moderation_reason)='' THEN '' ELSE '：'||COALESCE(NULLIF(NEW.moderation_reason,''),OLD.moderation_reason) END,
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER resources_moderation_message AFTER UPDATE OF moderation_state ON resources FOR EACH ROW EXECUTE FUNCTION notify_resource_moderation();
CREATE FUNCTION notify_report_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind IN ('report','resource_report','comment_report') AND OLD.status<>NEW.status THEN
  INSERT INTO user_messages(id,user_id,kind,title,body,ref) VALUES(md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'report_result','举报处理结果',CASE WHEN NEW.resolution='' THEN '举报处理状态已更新为 '||NEW.status ELSE NEW.resolution END,NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
DO $$ BEGIN IF to_regclass('feedback_tickets') IS NOT NULL THEN EXECUTE 'CREATE TRIGGER feedback_report_result_message AFTER UPDATE OF status ON feedback_tickets FOR EACH ROW EXECUTE FUNCTION notify_report_result()'; END IF; END $$;
`

// Migrate creates and updates the database schema.
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
		switch installedVersion.Int64 {
		case 1:
			if err := migrateV2(ctx, tx); err != nil {
				return err
			}
			return migrateV3(ctx, db, true)
		case 2:
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("release PostgreSQL schema lock: %w", err)
			}
			return migrateV3(ctx, db, true)
		case 3:
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("release PostgreSQL schema lock: %w", err)
			}
			return migrateV4(ctx, db, true)
		case 4:
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("release PostgreSQL schema lock: %w", err)
			}
			return migrateV5(ctx, db, true)
		case 5:
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("release PostgreSQL schema lock: %w", err)
			}
			return migrateV6(ctx, db, true)
		case 6:
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("release PostgreSQL schema lock: %w", err)
			}
			return migrateV7(ctx, db, true)
		case 7:
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("release PostgreSQL schema lock: %w", err)
			}
			return migrateV8(ctx, db)
		default:
			return fmt.Errorf("database schema version %d is unsupported; drop and recreate the database for schema version %d", installedVersion.Int64, schemaVersion)
		}
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
 banned_at timestamptz, ban_reason text NOT NULL DEFAULT '', creator_frozen_at timestamptz,
 last_announcement_read_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
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
 moderation_state text NOT NULL DEFAULT 'visible' CHECK (moderation_state IN ('visible','suspended','frozen')),
 moderation_by text CHECK (moderation_by IN ('owner','admin')),
 moderation_reason text NOT NULL DEFAULT '',
 moderation_at timestamptz, download_count integer NOT NULL DEFAULT 0,
 current_revision_id uuid, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(owner_id,slug)
);
CREATE INDEX resources_governance_idx ON resources(moderation_state,updated_at DESC);
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
 kind text NOT NULL CHECK (kind IN ('feedback','report','resource_report','comment_report')), subject text NOT NULL, message text NOT NULL,
 target_source text NOT NULL DEFAULT '', target_id text NOT NULL DEFAULT '', target_url text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','investigating','replied','resolved','dismissed','closed')),
 resolution text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), closed_at timestamptz
);
CREATE INDEX feedback_tickets_user_idx ON feedback_tickets(user_id,updated_at DESC);
CREATE INDEX feedback_tickets_status_idx ON feedback_tickets(status,updated_at DESC);
CREATE INDEX feedback_tickets_admin_idx ON feedback_tickets(kind,status,target_source,updated_at DESC);
CREATE INDEX feedback_tickets_target_idx ON feedback_tickets(target_source,target_id) WHERE kind IN ('report','resource_report','comment_report');
CREATE TABLE feedback_replies (
 id uuid PRIMARY KEY, ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
 author_id uuid REFERENCES users(id) ON DELETE SET NULL, message text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_replies_ticket_idx ON feedback_replies(ticket_id,created_at);
CREATE TABLE download_events (
 id bigserial PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 artifact_id uuid REFERENCES revision_artifacts(id) ON DELETE SET NULL,
 user_id uuid REFERENCES users(id) ON DELETE SET NULL, ip_hash text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX download_events_dedup_idx ON download_events(resource_id,user_id,ip_hash,created_at DESC);
CREATE TABLE server_settings (
 key text PRIMARY KEY, value text NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE resource_comments (
 id uuid PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 parent_id uuid REFERENCES resource_comments(id) ON DELETE CASCADE,
 body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
 moderation_state text NOT NULL DEFAULT 'visible' CHECK (moderation_state IN ('visible','hidden')),
 created_at timestamptz NOT NULL DEFAULT now(), edited_at timestamptz, deleted_at timestamptz
);
CREATE INDEX resource_comments_resource_idx ON resource_comments(resource_id,created_at DESC);
CREATE INDEX resource_comments_user_idx ON resource_comments(user_id,created_at DESC);
CREATE TABLE comment_moderation (
 comment_id uuid PRIMARY KEY REFERENCES resource_comments(id) ON DELETE CASCADE,
 provider text NOT NULL, model text NOT NULL, action text NOT NULL,
 categories text[] NOT NULL DEFAULT '{}', reason text NOT NULL DEFAULT '', raw_response jsonb NOT NULL DEFAULT '{}',
 human_reviewed_at timestamptz, human_action text NOT NULL DEFAULT ''
);
CREATE TABLE user_messages (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('review_result','moderation','comment_reply','report_result','admin_message','account','announcement')),
 title text NOT NULL, body text NOT NULL, ref text NOT NULL DEFAULT '', read_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '90 days'
);
CREATE INDEX user_messages_user_idx ON user_messages(user_id,created_at DESC);
CREATE TABLE announcements (
 id uuid PRIMARY KEY, title text NOT NULL, body text NOT NULL, published_at timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES users(id) ON DELETE SET NULL
);
CREATE TABLE app_releases (
 id uuid PRIMARY KEY, version text NOT NULL, channel text NOT NULL DEFAULT 'stable', platform text NOT NULL DEFAULT 'all', arch text NOT NULL DEFAULT 'all',
 minimum_version text NOT NULL DEFAULT '', notes_zh text NOT NULL DEFAULT '', notes_en text NOT NULL DEFAULT '', download_url text NOT NULL,
 published_at timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 UNIQUE(version,channel,platform,arch)
);
CREATE INDEX app_releases_lookup_idx ON app_releases(channel,platform,arch,published_at DESC);
`
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v%d: %w", schemaVersion, err)
	}
	if _, err := tx.ExecContext(ctx, notificationSchema); err != nil {
		return fmt.Errorf("install PostgreSQL notification triggers: %w", err)
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

// migrateV2 unifies the resource state machine: the resources.state lifecycle
// column folds into moderation_state, which gains the 'frozen' value plus
// actor/reason/timestamp bookkeeping. Archived resources become owner
// takedowns; previously suspended resources are attributed to admins.
func migrateV2(ctx context.Context, tx *sql.Tx) error {
	const migration = `
ALTER TABLE resources ADD COLUMN moderation_by text;
ALTER TABLE resources ADD COLUMN moderation_reason text NOT NULL DEFAULT '';
ALTER TABLE resources ADD COLUMN moderation_at timestamptz;
UPDATE resources SET moderation_state='suspended',moderation_by='owner',moderation_at=COALESCE(updated_at,now()) WHERE state='archived';
UPDATE resources SET moderation_by='admin',moderation_at=COALESCE(updated_at,now()) WHERE moderation_state='suspended' AND moderation_by IS NULL;
ALTER TABLE resources DROP COLUMN state;
ALTER TABLE resources ALTER COLUMN moderation_state SET DEFAULT 'visible';
ALTER TABLE resources DROP CONSTRAINT resources_moderation_state_check;
ALTER TABLE resources ADD CONSTRAINT resources_moderation_state_check CHECK (moderation_state IN ('visible','suspended','frozen'));
ALTER TABLE resources ADD CONSTRAINT resources_moderation_by_check CHECK (moderation_by IN ('owner','admin'));
DROP INDEX IF EXISTS resources_governance_idx;
CREATE INDEX resources_governance_idx ON resources(moderation_state,updated_at DESC);
`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v2 migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(2)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v2: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v2 migration: %w", err)
	}
	return nil
}

// migrateV3 adds download accounting: a per-resource counter plus the event
// log used for rate limiting and 24-hour deduplication. When chain is true it
// continues to the next migration instead of stopping at v3.
func migrateV3(ctx context.Context, db *sql.DB, chain bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v3 transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	const migration = `
ALTER TABLE resources ADD COLUMN download_count integer NOT NULL DEFAULT 0;
CREATE TABLE download_events (
 id bigserial PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 artifact_id uuid REFERENCES revision_artifacts(id) ON DELETE SET NULL,
 user_id uuid REFERENCES users(id) ON DELETE SET NULL, ip_hash text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX download_events_dedup_idx ON download_events(resource_id,user_id,ip_hash,created_at DESC);
`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v3 migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(3)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v3: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v3 migration: %w", err)
	}
	if chain {
		return migrateV4(ctx, db, true)
	}
	return nil
}

// migrateV4 adds user governance columns: account bans and creator freezes.
// When chain is true it continues to the next migration.
func migrateV4(ctx context.Context, db *sql.DB, chain bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v4 transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	const migration = `
ALTER TABLE users ADD COLUMN banned_at timestamptz;
ALTER TABLE users ADD COLUMN ban_reason text NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN creator_frozen_at timestamptz;
`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v4 migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(4)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v4: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v4 migration: %w", err)
	}
	if chain {
		return migrateV5(ctx, db, true)
	}
	return nil
}

// migrateV5 adds the comment system: comments, AI moderation records and the
// key-value settings table that holds the moderation prompt.
func migrateV5(ctx context.Context, db *sql.DB, chain bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v5 transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	const migration = `
CREATE TABLE server_settings (
 key text PRIMARY KEY, value text NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE resource_comments (
 id uuid PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 parent_id uuid REFERENCES resource_comments(id) ON DELETE CASCADE,
 body text NOT NULL CHECK (char_length(body) BETWEEN 1 AND 2000),
 moderation_state text NOT NULL DEFAULT 'visible' CHECK (moderation_state IN ('visible','hidden')),
 created_at timestamptz NOT NULL DEFAULT now(), edited_at timestamptz, deleted_at timestamptz
);
CREATE INDEX resource_comments_resource_idx ON resource_comments(resource_id,created_at DESC);
CREATE INDEX resource_comments_user_idx ON resource_comments(user_id,created_at DESC);
CREATE TABLE comment_moderation (
 comment_id uuid PRIMARY KEY REFERENCES resource_comments(id) ON DELETE CASCADE,
 provider text NOT NULL, model text NOT NULL, action text NOT NULL,
 categories text[] NOT NULL DEFAULT '{}', reason text NOT NULL DEFAULT '', raw_response jsonb NOT NULL DEFAULT '{}',
 human_reviewed_at timestamptz, human_action text NOT NULL DEFAULT ''
);
`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v5 migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(5)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v5: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v5 migration: %w", err)
	}
	if chain {
		return migrateV6(ctx, db, true)
	}
	return nil
}

func migrateV6(ctx context.Context, db *sql.DB, chain bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v6 transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	const migration = `
ALTER TABLE users ADD COLUMN last_announcement_read_at timestamptz;
CREATE TABLE user_messages (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('review_result','moderation','comment_reply','report_result','admin_message','account','announcement')),
 title text NOT NULL, body text NOT NULL, ref text NOT NULL DEFAULT '', read_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '90 days'
);
CREATE INDEX user_messages_user_idx ON user_messages(user_id,created_at DESC);
CREATE TABLE announcements (
 id uuid PRIMARY KEY, title text NOT NULL, body text NOT NULL, published_at timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES users(id) ON DELETE SET NULL
);`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v6 migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(6)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v6: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v6 migration: %w", err)
	}
	if chain {
		return migrateV7(ctx, db, true)
	}
	return nil
}

func migrateV7(ctx context.Context, db *sql.DB, chain bool) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v7 transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	const migration = `CREATE TABLE app_releases (
 id uuid PRIMARY KEY, version text NOT NULL, channel text NOT NULL DEFAULT 'stable', platform text NOT NULL DEFAULT 'all', arch text NOT NULL DEFAULT 'all',
 minimum_version text NOT NULL DEFAULT '', notes_zh text NOT NULL DEFAULT '', notes_en text NOT NULL DEFAULT '', download_url text NOT NULL,
 published_at timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 UNIQUE(version,channel,platform,arch)
); CREATE INDEX app_releases_lookup_idx ON app_releases(channel,platform,arch,published_at DESC);`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v7 migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, notificationSchema); err != nil {
		return fmt.Errorf("install PostgreSQL notification triggers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(7)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v7: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v7 migration: %w", err)
	}
	if chain {
		return migrateV8(ctx, db)
	}
	return nil
}

// migrateV8 distinguishes resource and comment reports at the database layer
// and makes result notifications cover both report types.
func migrateV8(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin PostgreSQL schema v8 transaction: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext('oronbox-server-schema'))`); err != nil {
		return fmt.Errorf("lock PostgreSQL schema: %w", err)
	}
	const migration = `
ALTER TABLE feedback_tickets DROP CONSTRAINT feedback_tickets_kind_check;
ALTER TABLE feedback_tickets ADD CONSTRAINT feedback_tickets_kind_check CHECK (kind IN ('feedback','report','resource_report','comment_report'));
DROP INDEX feedback_tickets_target_idx;
CREATE INDEX feedback_tickets_target_idx ON feedback_tickets(target_source,target_id) WHERE kind IN ('report','resource_report','comment_report');
CREATE OR REPLACE FUNCTION notify_report_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind IN ('report','resource_report','comment_report') AND OLD.status<>NEW.status THEN
  INSERT INTO user_messages(id,user_id,kind,title,body,ref) VALUES(md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'report_result','举报处理结果',CASE WHEN NEW.resolution='' THEN '举报处理状态已更新为 '||NEW.status ELSE NEW.resolution END,NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
`
	if _, err := tx.ExecContext(ctx, migration); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v8 migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(8)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v8: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit PostgreSQL schema v8 migration: %w", err)
	}
	return nil
}
