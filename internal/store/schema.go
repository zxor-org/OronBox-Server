package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SchemaSQL defines the authoritative plain PostgreSQL DDL for OronBox-Server.
// It covers 40 core dynamic tables across 9 domains with zero UUID primary keys.
// All tables are created idempotently and safely.
const SchemaSQL = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 1. 用户与 OAuth 鉴权配套域 (8 表)
CREATE TABLE IF NOT EXISTS users (
  bandbbs_uid integer PRIMARY KEY,
  username varchar(64) NOT NULL,
  avatar_url text,
  role varchar(16) NOT NULL DEFAULT 'user',
  banned boolean NOT NULL DEFAULT false,
  ban_reason text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS oauth_states (
  id varchar(128) PRIMARY KEY,
  provider varchar(32) NOT NULL,
  purpose varchar(32) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  app_id varchar(64) NOT NULL,
  platform varchar(32) NOT NULL,
  return_uri varchar(512) NOT NULL,
  user_uid integer REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  ip varchar(45),
  user_agent text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS oauth_events (
  id bigserial PRIMARY KEY,
  created_at timestamptz NOT NULL DEFAULT now(),
  provider varchar(32) NOT NULL,
  event_type varchar(32) NOT NULL,
  result varchar(32) NOT NULL,
  ip varchar(45) NOT NULL DEFAULT '',
  error_code varchar(64) NOT NULL DEFAULT '',
  error_message text NOT NULL DEFAULT '',
  latency_ms bigint NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS oauth_grants (
  id bigserial PRIMARY KEY,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  provider varchar(32) NOT NULL,
  subject varchar(64) NOT NULL,
  scopes text[] NOT NULL DEFAULT '{}',
  access_token_cipher bytea NOT NULL,
  refresh_token_cipher bytea NOT NULL,
  expires_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_uid, provider)
);

CREATE TABLE IF NOT EXISTS login_tickets (
  id bigserial PRIMARY KEY,
  ticket_hash bytea UNIQUE NOT NULL,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  used_at timestamptz
);

CREATE TABLE IF NOT EXISTS sessions (
  id bigserial PRIMARY KEY,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  access_hash bytea UNIQUE NOT NULL,
  refresh_hash bytea UNIQUE NOT NULL,
  access_expires_at timestamptz NOT NULL,
  refresh_expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  platform varchar(32) NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_sessions (
  id bigserial PRIMARY KEY,
  session_hash bytea UNIQUE NOT NULL,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  username varchar(64) NOT NULL,
  ip varchar(45),
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS github_grants (
  user_uid integer PRIMARY KEY REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  github_user_id bigint NOT NULL,
  login varchar(64) NOT NULL,
  access_token_cipher bytea NOT NULL,
  scopes text[] NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- 2. 资源主实体与互动配套域 (7 表)
CREATE TABLE IF NOT EXISTS resources (
  resource_id varchar(128) PRIMARY KEY,
  title varchar(128) NOT NULL,
  summary text NOT NULL,
  description text NOT NULL DEFAULT '',
  restype varchar(32) NOT NULL DEFAULT 'quick_app',
  authors jsonb NOT NULL DEFAULT '[]'::jsonb,
  owner_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE RESTRICT,
  devices jsonb NOT NULL DEFAULT '[]'::jsonb,
  tags jsonb NOT NULL DEFAULT '[]'::jsonb,
  paid_type varchar(32) NOT NULL DEFAULT '',
  icon_sha256 char(64) NOT NULL DEFAULT '',
  cover_sha256 char(64) NOT NULL DEFAULT '',
  preview_shas jsonb NOT NULL DEFAULT '[]'::jsonb,
  moderation_state varchar(32) NOT NULL DEFAULT 'visible',
  moderation_reason text NOT NULL DEFAULT '',
  moderation_by varchar(16),
  curation_grade varchar(32) NOT NULL DEFAULT 'standard',
  current_revision_id bigint,
  download_count integer NOT NULL DEFAULT 0,
  coin_count integer NOT NULL DEFAULT 0,
  rating double precision NOT NULL DEFAULT 5.0,
  rating_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz,
  last_released_at timestamptz
);

CREATE TABLE IF NOT EXISTS resource_sources (
  resource_id varchar(128) PRIMARY KEY REFERENCES resources(resource_id) ON DELETE CASCADE,
  author_name varchar(256) NOT NULL DEFAULT '',
  source_url varchar(1024) NOT NULL DEFAULT '',
  license_name varchar(128) NOT NULL DEFAULT '',
  authorization_note text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS resource_ratings (
  resource_id varchar(128) NOT NULL REFERENCES resources(resource_id) ON DELETE CASCADE,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  rating smallint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (resource_id, user_uid)
);

CREATE TABLE IF NOT EXISTS resource_collections (
  id varchar(128) PRIMARY KEY,
  owner_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  kind varchar(32) NOT NULL DEFAULT 'quick_app',
  current_revision_id bigint,
  coin_count integer NOT NULL DEFAULT 0,
  moderation_state varchar(32) NOT NULL DEFAULT 'visible',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  published_at timestamptz
);

CREATE TABLE IF NOT EXISTS resource_collection_revisions (
  id bigserial PRIMARY KEY,
  collection_id varchar(128) NOT NULL REFERENCES resource_collections(id) ON DELETE CASCADE,
  revision_no integer NOT NULL,
  name varchar(128) NOT NULL,
  summary text NOT NULL DEFAULT '',
  description text NOT NULL DEFAULT '',
  icon_sha256 char(64) NOT NULL DEFAULT '',
  cover_sha256 char(64) NOT NULL DEFAULT '',
  representative_resource_id varchar(128),
  items jsonb NOT NULL DEFAULT '[]'::jsonb,
  state varchar(32) NOT NULL DEFAULT 'draft',
  reviewer_uid integer,
  review_note text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(collection_id, revision_no)
);

CREATE TABLE IF NOT EXISTS tags (
  id serial PRIMARY KEY,
  name varchar(32) UNIQUE NOT NULL,
  coefficient numeric(4, 2) NOT NULL DEFAULT 1.00,
  enabled boolean NOT NULL DEFAULT true,
  sort_order integer NOT NULL DEFAULT 0,
  usage_count integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS resource_comments (
  id bigserial PRIMARY KEY,
  resource_id varchar(128) NOT NULL REFERENCES resources(resource_id) ON DELETE CASCADE,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  parent_id bigint REFERENCES resource_comments(id) ON DELETE SET NULL,
  content text NOT NULL,
  state varchar(16) NOT NULL DEFAULT 'visible',
  is_deleted boolean NOT NULL DEFAULT false,
  moderation_reason text,
  ai_action varchar(16) NOT NULL DEFAULT 'pass',
  ai_reason text NOT NULL DEFAULT '',
  ai_model varchar(64) NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS resource_coin_votes (
  resource_id varchar(128) NOT NULL REFERENCES resources(resource_id) ON DELETE CASCADE,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  amount integer NOT NULL DEFAULT 10 CHECK(amount > 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (resource_id, user_uid)
);

-- 3. 资源版本生命周期与发布联发域 (5 表)
CREATE TABLE IF NOT EXISTS resource_revisions (
  id bigserial PRIMARY KEY,
  resource_id varchar(128) NOT NULL REFERENCES resources(resource_id) ON DELETE CASCADE,
  revision_no integer NOT NULL DEFAULT 1,
  version varchar(32) NOT NULL,
  name varchar(128) NOT NULL DEFAULT '',
  summary text NOT NULL DEFAULT '',
  changelog text NOT NULL DEFAULT '',
  downloads jsonb NOT NULL DEFAULT '{}'::jsonb,
  config jsonb NOT NULL DEFAULT '{}'::jsonb,
  state varchar(32) NOT NULL DEFAULT 'draft',
  creator_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE RESTRICT,
  reviewer_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  review_note text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  reviewed_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(resource_id, version)
);

CREATE TABLE IF NOT EXISTS resource_collaborators (
  resource_id varchar(128) NOT NULL REFERENCES resources(resource_id) ON DELETE CASCADE,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  role varchar(32) NOT NULL DEFAULT 'collaborator',
  status varchar(16) NOT NULL DEFAULT 'pending',
  invited_by_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (resource_id, user_uid)
);

CREATE TABLE IF NOT EXISTS publications (
  id bigserial PRIMARY KEY,
  revision_id bigint NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
  provider varchar(32) NOT NULL,
  category_id integer NOT NULL DEFAULT 0,
  state varchar(32) NOT NULL DEFAULT 'pending',
  config jsonb NOT NULL DEFAULT '{}'::jsonb,
  external_id varchar(128),
  external_url varchar(512),
  error_message text,
  status_detail jsonb,
  attempts integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(revision_id, provider, category_id)
);

CREATE TABLE IF NOT EXISTS publication_attempts (
  id bigserial PRIMARY KEY,
  publication_id bigint NOT NULL REFERENCES publications(id) ON DELETE CASCADE,
  attempt_number integer NOT NULL,
  phase varchar(16) NOT NULL,
  state_from varchar(32) NOT NULL,
  state_to varchar(32) NOT NULL,
  error_message text NOT NULL DEFAULT '',
  detail jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS external_bindings (
  id bigserial PRIMARY KEY,
  resource_id varchar(128) NOT NULL REFERENCES resources(resource_id) ON DELETE CASCADE,
  provider varchar(32) NOT NULL,
  category_id integer NOT NULL DEFAULT 0,
  external_id varchar(128) NOT NULL,
  external_url varchar(512) NOT NULL DEFAULT '',
  meta jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(resource_id, provider, category_id),
  UNIQUE(provider, external_id)
);

-- 4. 审核治理与申诉配套域 (4 表)
CREATE TABLE IF NOT EXISTS review_cases (
  id bigserial PRIMARY KEY,
  revision_id bigint UNIQUE NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
  state varchar(16) NOT NULL DEFAULT 'pending',
  priority integer NOT NULL DEFAULT 0,
  assigned_reviewer_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  checklist jsonb NOT NULL DEFAULT '[]'::jsonb,
  note text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS review_case_events (
  id bigserial PRIMARY KEY,
  case_id bigint NOT NULL REFERENCES review_cases(id) ON DELETE CASCADE,
  actor_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  event varchar(32) NOT NULL,
  note text NOT NULL DEFAULT '',
  checklist jsonb NOT NULL DEFAULT '[]'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS review_appeals (
  id bigserial PRIMARY KEY,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  subject_type varchar(32) NOT NULL,
  subject_id varchar(128) NOT NULL,
  message text NOT NULL,
  status varchar(16) NOT NULL DEFAULT 'open',
  resolution text NOT NULL DEFAULT '',
  handled_by_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS moderation_reason_templates (
  id bigserial PRIMARY KEY,
  scope varchar(32) NOT NULL,
  decision varchar(16) NOT NULL DEFAULT '',
  title varchar(128) NOT NULL,
  body text NOT NULL,
  position integer NOT NULL DEFAULT 0,
  enabled boolean NOT NULL DEFAULT true
);

-- 5. 单用户专属私密消息箱域 (1 表)
CREATE TABLE IF NOT EXISTS user_messages (
  id bigserial PRIMARY KEY,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  kind varchar(32) NOT NULL DEFAULT 'system',
  event varchar(64) NOT NULL DEFAULT '',
  data jsonb NOT NULL DEFAULT '{}'::jsonb,
  title varchar(128),
  body text,
  ref varchar(128),
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz
);

-- 6. 硬币经济与借贷账本域 (3 表)
CREATE TABLE IF NOT EXISTS user_coin_accounts (
  user_uid integer PRIMARY KEY REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  balance integer NOT NULL DEFAULT 0 CHECK(balance >= 0),
  last_checkin_date date,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS daily_coin_checkins (
  id bigserial PRIMARY KEY,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  checkin_date date NOT NULL,
  coins_awarded integer NOT NULL DEFAULT 10,
  streak_days integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_uid, checkin_date)
);

CREATE TABLE IF NOT EXISTS coin_ledger (
  id bigserial PRIMARY KEY,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  delta_units integer NOT NULL,
  kind varchar(32) NOT NULL,
  reference_type varchar(32),
  reference_id varchar(128),
  note text NOT NULL DEFAULT '',
  balance_after integer NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- 7. 客服与工单配套域 (4 表)
CREATE TABLE IF NOT EXISTS feedback_tickets (
  id bigserial PRIMARY KEY,
  user_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  kind varchar(32) NOT NULL DEFAULT 'defect',
  subject varchar(128) NOT NULL,
  message text NOT NULL,
  resolution text NOT NULL DEFAULT '',
  target_source varchar(32) NOT NULL DEFAULT 'app',
  target_id varchar(128) NOT NULL DEFAULT '',
  target_url varchar(255) NOT NULL DEFAULT '',
  status varchar(32) NOT NULL DEFAULT 'open',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS feedback_replies (
  id bigserial PRIMARY KEY,
  ticket_id bigint NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
  author_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  message text NOT NULL,
  is_admin boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS feedback_internal_notes (
  id bigserial PRIMARY KEY,
  ticket_id bigint NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
  author_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE CASCADE,
  message text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS feedback_status_history (
  id bigserial PRIMARY KEY,
  ticket_id bigint NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
  actor_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  from_status varchar(16) NOT NULL,
  to_status varchar(16) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- 8. 插件生态与客户端底座域 (6 表)
CREATE TABLE IF NOT EXISTS plugins (
  id varchar(128) PRIMARY KEY,
  name varchar(128) NOT NULL,
  author varchar(128) NOT NULL,
  description text NOT NULL DEFAULT '',
  uploader_uid integer NOT NULL REFERENCES users(bandbbs_uid) ON DELETE RESTRICT,
  runtime varchar(32) NOT NULL DEFAULT 'js',
  permissions jsonb NOT NULL DEFAULT '[]'::jsonb,
  state varchar(32) NOT NULL DEFAULT 'visible',
  moderation_reason text NOT NULL DEFAULT '',
  current_version_id bigint,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS plugin_versions (
  id bigserial PRIMARY KEY,
  plugin_id varchar(128) NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,
  version varchar(64) NOT NULL,
  changelog text NOT NULL DEFAULT '',
  package_sha256 char(64) NOT NULL DEFAULT '',
  state varchar(32) NOT NULL DEFAULT 'pending',
  reviewer_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  review_note text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  reviewed_at timestamptz,
  UNIQUE(plugin_id, version)
);

CREATE TABLE IF NOT EXISTS app_releases (
  version varchar(32) PRIMARY KEY,
  minimum_version varchar(32) NOT NULL DEFAULT '',
  notes_zh text NOT NULL DEFAULT '',
  notes_en text NOT NULL DEFAULT '',
  published_at timestamptz NOT NULL,
  synced_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS download_events (
  id bigserial PRIMARY KEY,
  resource_id varchar(128) NOT NULL,
  device varchar(32) NOT NULL DEFAULT '',
  user_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  ip varchar(45) NOT NULL,
  user_agent text NOT NULL DEFAULT '',
  download_date date NOT NULL DEFAULT CURRENT_DATE,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(resource_id, ip, device, download_date)
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id bigserial PRIMARY KEY,
  actor_uid integer REFERENCES users(bandbbs_uid) ON DELETE SET NULL,
  action varchar(64) NOT NULL,
  result varchar(32) NOT NULL,
  ip varchar(45),
  target_data jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS server_settings (
  key varchar(64) PRIMARY KEY,
  value jsonb NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- 9. 纯 Blob 与多端存储底座域 (2 表)
CREATE TABLE IF NOT EXISTS blobs (
  sha256 char(64) PRIMARY KEY,
  size_bytes bigint NOT NULL,
  media_type varchar(64) NOT NULL DEFAULT 'application/octet-stream',
  ref_count integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS blob_replicas (
  blob_sha256 char(64) NOT NULL REFERENCES blobs(sha256) ON DELETE CASCADE,
  backend varchar(32) NOT NULL,
  object_key varchar(255) NOT NULL DEFAULT '',
  state varchar(16) NOT NULL DEFAULT 'ready',
  error_message text NOT NULL DEFAULT '',
  attempts integer NOT NULL DEFAULT 1,
  next_attempt_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (blob_sha256, backend)
);

-- 核心高性能索引
CREATE INDEX IF NOT EXISTS idx_resources_moderation_last_released ON resources(moderation_state, last_released_at DESC);
CREATE INDEX IF NOT EXISTS idx_resources_owner_uid ON resources(owner_uid);
CREATE INDEX IF NOT EXISTS idx_resource_ratings_resource_id ON resource_ratings(resource_id);
CREATE INDEX IF NOT EXISTS idx_resource_collections_owner_mod ON resource_collections(owner_uid, moderation_state);
CREATE INDEX IF NOT EXISTS idx_resource_coll_rev_state ON resource_collection_revisions(collection_id, state);
CREATE INDEX IF NOT EXISTS idx_tags_enabled_usage_sort ON tags(enabled, usage_count, sort_order);
CREATE INDEX IF NOT EXISTS idx_resource_comments_res_created ON resource_comments(resource_id, created_at);
CREATE INDEX IF NOT EXISTS idx_resource_coin_votes_res_id ON resource_coin_votes(resource_id);
CREATE INDEX IF NOT EXISTS idx_resource_revisions_creator_state ON resource_revisions(creator_uid, state, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_resource_collaborators_user_status ON resource_collaborators(user_uid, status);
CREATE INDEX IF NOT EXISTS idx_publications_rev_state ON publications(revision_id, state);
CREATE INDEX IF NOT EXISTS idx_user_messages_read_created ON user_messages(user_uid, kind, read_at, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_coin_ledger_user_created ON coin_ledger(user_uid, created_at);
CREATE INDEX IF NOT EXISTS idx_feedback_tickets_user_status ON feedback_tickets(user_uid, status);
CREATE INDEX IF NOT EXISTS idx_plugins_uploader ON plugins(uploader_uid);
CREATE INDEX IF NOT EXISTS idx_plugins_state ON plugins(state);
CREATE INDEX IF NOT EXISTS idx_plugin_versions_plugin_state ON plugin_versions(plugin_id, state);
CREATE INDEX IF NOT EXISTS idx_app_releases_published ON app_releases(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_download_events_res_created ON download_events(resource_id, created_at);
`

func Migrate(ctx context.Context, s *Store) error {
	if s == nil || s.Pool == nil {
		return fmt.Errorf("store is not initialized")
	}
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(4242)`); err != nil {
			return err
		}
		for _, statement := range strings.Split(SchemaSQL, ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if _, err := tx.Exec(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	})
}

// applySchema executes SchemaSQL statements on the pool without transaction wrapper.
func applySchema(ctx context.Context, s *Store) error {
	for _, statement := range strings.Split(SchemaSQL, ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := s.Pool.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func Schema(ctx context.Context, s *Store) error {
	if s == nil || s.Pool == nil {
		return fmt.Errorf("store is not initialized")
	}
	if err := applySchema(ctx, s); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}
