
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
	enabled boolean NOT NULL DEFAULT true, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX devices_astrobox_id_unique_idx ON devices(astrobox_id) WHERE astrobox_id<>'';
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
CREATE TABLE plugins (
 id text PRIMARY KEY CHECK (id ~ '^[a-z][a-z0-9]*([.-][a-z0-9][a-z0-9-]*)+$'),
 uploader_id uuid NOT NULL REFERENCES users(id),
 name text NOT NULL, version text NOT NULL, author text NOT NULL DEFAULT '',
 description text NOT NULL DEFAULT '', runtime text NOT NULL CHECK (runtime IN ('js','wasm','hybrid')),
 permissions jsonb NOT NULL DEFAULT '[]',
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','listed','rejected','delisted')),
 moderation_reason text NOT NULL DEFAULT '',
 package_sha256 char(64) NOT NULL REFERENCES blobs(sha256),
 icon_sha256 char(64) REFERENCES blobs(sha256),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE plugin_versions (
 id uuid PRIMARY KEY,plugin_id text NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,revision_no integer NOT NULL,
 version text NOT NULL,name text NOT NULL,author text NOT NULL DEFAULT '',description text NOT NULL DEFAULT '',
 runtime text NOT NULL CHECK (runtime IN ('js','wasm','hybrid')),permissions jsonb NOT NULL DEFAULT '[]',
 package_sha256 char(64) NOT NULL REFERENCES blobs(sha256),icon_sha256 char(64) REFERENCES blobs(sha256),
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','listed','rejected','superseded')),
 moderation_reason text NOT NULL DEFAULT '',created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 created_via text NOT NULL DEFAULT 'uploader' CHECK (created_via IN ('uploader','admin','import')),
 base_version_id uuid REFERENCES plugin_versions(id) ON DELETE SET NULL,created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),UNIQUE(plugin_id,revision_no)
);
ALTER TABLE plugins ADD COLUMN current_version_id uuid REFERENCES plugin_versions(id) ON DELETE SET NULL,ADD COLUMN pending_version_id uuid REFERENCES plugin_versions(id) ON DELETE SET NULL;
CREATE TABLE blog_posts (
 slug text PRIMARY KEY CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,63}$'),
 type text NOT NULL DEFAULT 'announcement' CHECK (type IN ('announcement','recommendation','docs')),
 title text NOT NULL, subtitle text NOT NULL DEFAULT '', author text NOT NULL DEFAULT '',
 cover_sha256 char(64) REFERENCES blobs(sha256),
 body text NOT NULL DEFAULT '',
 published boolean NOT NULL DEFAULT false, published_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE resources (
 id uuid PRIMARY KEY, owner_id uuid NOT NULL REFERENCES users(id), slug text NOT NULL, draft_name text NOT NULL DEFAULT '',
 platform text NOT NULL DEFAULT 'vela_os' CHECK (platform='vela_os'),
 kind text NOT NULL CHECK (kind IN ('quickapp','watchface')),
	moderation_state text NOT NULL DEFAULT 'visible' CHECK (moderation_state IN ('visible','suspended','frozen','deleted')),
 moderation_by text CHECK (moderation_by IN ('owner','admin')),
 moderation_reason text NOT NULL DEFAULT '',
 moderation_at timestamptz, download_count integer NOT NULL DEFAULT 0,
 curation_grade text NOT NULL DEFAULT 'standard' CHECK (curation_grade IN ('standard','featured')),
	 current_revision_id uuid, published_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(owner_id,slug)
);
CREATE INDEX resources_governance_idx ON resources(moderation_state,updated_at DESC);
CREATE TABLE home_sections (
 id text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9-]{1,31}$'),
 name text NOT NULL, description text NOT NULL DEFAULT '',
 position integer NOT NULL DEFAULT 0, enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE home_section_cards (
 id uuid PRIMARY KEY,
 section_id text NOT NULL REFERENCES home_sections(id) ON DELETE CASCADE,
 type text NOT NULL CHECK (type IN ('resource','blog')),
 resource_id uuid REFERENCES resources(id) ON DELETE CASCADE,
 blog_slug text REFERENCES blog_posts(slug) ON DELETE CASCADE,
 position integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((type='resource' AND resource_id IS NOT NULL AND blog_slug IS NULL) OR (type='blog' AND resource_id IS NULL AND blog_slug IS NOT NULL))
);
CREATE TABLE home_banners (
 id uuid PRIMARY KEY,
 type text NOT NULL CHECK (type IN ('resource','blog','link')),
 title text NOT NULL, subtitle text NOT NULL DEFAULT '',
 cover_sha256 char(64) REFERENCES blobs(sha256),
 resource_id uuid REFERENCES resources(id) ON DELETE CASCADE,
 blog_slug text REFERENCES blog_posts(slug) ON DELETE CASCADE,
 link_url text NOT NULL DEFAULT '',
 position integer NOT NULL DEFAULT 0, enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((type='resource' AND resource_id IS NOT NULL AND blog_slug IS NULL AND link_url='') OR (type='blog' AND resource_id IS NULL AND blog_slug IS NOT NULL AND link_url='') OR (type='link' AND resource_id IS NULL AND blog_slug IS NULL AND link_url<>''))
);
CREATE TABLE resource_revisions (
 id uuid PRIMARY KEY, resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 revision_no integer NOT NULL, name text NOT NULL, summary text NOT NULL,
 state text NOT NULL DEFAULT 'submitted' CHECK (state IN ('draft','submitted','approved','rejected','superseded')),
	paid_type text NOT NULL DEFAULT 'free' CHECK (paid_type IN ('free','paid','force_paid')),
	purchase_link text NOT NULL DEFAULT '', purchase_price numeric(12,2), purchase_currency text NOT NULL DEFAULT '',
	created_by uuid REFERENCES users(id) ON DELETE SET NULL,
	created_via text NOT NULL DEFAULT 'creator' CHECK (created_via IN ('creator','admin','import')),
	base_revision_id uuid REFERENCES resource_revisions(id) ON DELETE SET NULL,
	governance_source jsonb NOT NULL DEFAULT '{}', governance_collection_id uuid,
	governance_collection_position integer NOT NULL DEFAULT 0,
 publication_plan jsonb NOT NULL DEFAULT '[]',
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(resource_id,revision_no)
);
CREATE INDEX resource_revisions_resource_state_idx ON resource_revisions(resource_id,state,revision_no DESC);
ALTER TABLE resources ADD CONSTRAINT resources_current_revision_fk FOREIGN KEY (current_revision_id) REFERENCES resource_revisions(id);
CREATE TABLE resource_collections (
 id uuid PRIMARY KEY, owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 slug text NOT NULL, platform text NOT NULL DEFAULT 'vela_os' CHECK (platform='vela_os'),
 kind text NOT NULL CHECK (kind IN ('quickapp','watchface')), current_revision_id uuid, enabled boolean NOT NULL DEFAULT true,
	 representative_resource_id uuid REFERENCES resources(id) ON DELETE SET NULL, published_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(owner_id,slug)
);
CREATE TABLE resource_collection_revisions (
 id uuid PRIMARY KEY, collection_id uuid NOT NULL REFERENCES resource_collections(id) ON DELETE CASCADE,
 revision_no integer NOT NULL, name text NOT NULL, summary text NOT NULL DEFAULT '', enabled boolean NOT NULL DEFAULT true,
 representative_resource_id uuid REFERENCES resources(id) ON DELETE SET NULL,
 created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 created_via text NOT NULL DEFAULT 'creator' CHECK (created_via IN ('creator','admin','import')),
 base_revision_id uuid REFERENCES resource_collection_revisions(id) ON DELETE SET NULL,
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','approved','rejected','superseded')),
 reviewer_id uuid REFERENCES users(id) ON DELETE SET NULL, review_note text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(collection_id,revision_no)
);
CREATE TABLE resource_collection_revision_members (
 id uuid PRIMARY KEY, revision_id uuid NOT NULL REFERENCES resource_collection_revisions(id) ON DELETE CASCADE,
 resource_id uuid REFERENCES resources(id) ON DELETE SET NULL, resource_slug text NOT NULL, resource_name text NOT NULL DEFAULT '',
 position integer NOT NULL CHECK (position>=0), UNIQUE(revision_id,resource_id), UNIQUE(revision_id,position)
);
ALTER TABLE resource_collections ADD CONSTRAINT resource_collections_current_revision_fk FOREIGN KEY (current_revision_id) REFERENCES resource_collection_revisions(id);
ALTER TABLE resource_revisions ADD CONSTRAINT resource_revisions_governance_collection_fk FOREIGN KEY (governance_collection_id) REFERENCES resource_collections(id) ON DELETE SET NULL;
ALTER TABLE resources ADD COLUMN collection_id uuid REFERENCES resource_collections(id) ON DELETE SET NULL;
ALTER TABLE resources ADD COLUMN collection_position integer NOT NULL DEFAULT 0;
CREATE INDEX resources_collection_idx ON resources(collection_id,collection_position,created_at);
CREATE INDEX resource_collection_review_idx ON resource_collection_revisions(state,created_at) WHERE state='pending';
CREATE TABLE resource_collaborators (
 resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 invited_by uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 accepted_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(resource_id,user_id)
);
CREATE TABLE resource_revision_collaborators (
 revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, PRIMARY KEY(revision_id,user_id)
);
CREATE TABLE resource_sources (
 resource_id uuid PRIMARY KEY REFERENCES resources(id) ON DELETE CASCADE,
 author_name text NOT NULL DEFAULT '', source_url text NOT NULL DEFAULT '', license_name text NOT NULL DEFAULT '',
 authorization_note text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE resource_attributes (
 id text PRIMARY KEY CHECK (id ~ '^[a-z0-9][a-z0-9_-]{0,63}$'),
 name_zh text NOT NULL, name_en text NOT NULL DEFAULT '',
 coefficient numeric(8,4) NOT NULL DEFAULT 1.0000 CHECK (coefficient > 0 AND coefficient <= 10),
 enabled boolean NOT NULL DEFAULT true, position integer NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO resource_attributes(id,name_zh,name_en,position) VALUES
 ('original','原创','Original',10),('derivative','二创','Derivative',20),
 ('port','移植','Port',30),('template','模板','Template',40),
 ('ai_generated','AI 生成','AI generated',50);
CREATE TABLE resource_revision_attributes (
 revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 attribute text NOT NULL REFERENCES resource_attributes(id),
 PRIMARY KEY(revision_id,attribute)
);
CREATE TABLE revision_links (
 revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 position integer NOT NULL DEFAULT 0, title text NOT NULL, url text NOT NULL,
 PRIMARY KEY(revision_id,position)
);
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
 priority smallint NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 3),
 due_at timestamptz NOT NULL DEFAULT now()+interval '48 hours', first_submitted_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_cases_queue_idx ON review_cases(state,priority DESC,due_at,created_at) WHERE state='pending';
CREATE TABLE publications (
 id uuid PRIMARY KEY, revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,
 target text NOT NULL CHECK (target IN ('oronbox','bandbbs','astrobox')),
 state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','running','published','reviewing','failed','cancelled')),
 config jsonb NOT NULL DEFAULT '{}', external_id text NOT NULL DEFAULT '', external_url text NOT NULL DEFAULT '',
 error_message text NOT NULL DEFAULT '', status_detail jsonb NOT NULL DEFAULT '{}',
 attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_token uuid, lease_expires_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(revision_id,target)
);
CREATE INDEX publications_dispatch_idx ON publications(state,target,next_attempt_at);
CREATE TABLE publication_attempts (
 id bigserial PRIMARY KEY, publication_id uuid NOT NULL REFERENCES publications(id) ON DELETE CASCADE,
 attempt_number integer NOT NULL, phase text NOT NULL CHECK (phase IN ('execute','poll','admin')),
 event text NOT NULL, state_from text NOT NULL, state_to text NOT NULL,
 error_message text NOT NULL DEFAULT '', detail jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX publication_attempts_history_idx ON publication_attempts(publication_id,id DESC);
CREATE FUNCTION prevent_publication_attempt_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'publication attempt history is immutable'; END $$;
CREATE TRIGGER publication_attempts_immutable BEFORE UPDATE OR DELETE ON publication_attempts FOR EACH ROW EXECUTE FUNCTION prevent_publication_attempt_update();
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
 action text NOT NULL, result text NOT NULL, ip inet, user_agent text NOT NULL DEFAULT '', metadata jsonb NOT NULL DEFAULT '{}',
 before_data jsonb, after_data jsonb, target_data jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_logs_target_idx ON audit_logs((target_data->>'type'),(target_data->>'id'),id DESC) WHERE target_data->>'id'<>'';
CREATE TABLE feedback_tickets (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('feedback','resource_report','comment_report')), subject text NOT NULL, message text NOT NULL,
 target_source text NOT NULL DEFAULT '', target_id text NOT NULL DEFAULT '', target_url text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','investigating','replied','resolved','dismissed','closed')),
 resolution text NOT NULL DEFAULT '', target_snapshot jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), closed_at timestamptz
);
CREATE INDEX feedback_tickets_user_idx ON feedback_tickets(user_id,updated_at DESC);
CREATE INDEX feedback_tickets_status_idx ON feedback_tickets(status,updated_at DESC);
CREATE INDEX feedback_tickets_admin_idx ON feedback_tickets(kind,status,target_source,updated_at DESC);
CREATE INDEX feedback_tickets_target_idx ON feedback_tickets(target_source,target_id) WHERE kind IN ('resource_report','comment_report');
CREATE TABLE feedback_replies (
 id uuid PRIMARY KEY, ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
 author_id uuid REFERENCES users(id) ON DELETE SET NULL, message text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_replies_ticket_idx ON feedback_replies(ticket_id,created_at);
CREATE TABLE feedback_internal_notes (
 id uuid PRIMARY KEY, ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
 author_id uuid REFERENCES users(id) ON DELETE SET NULL, message text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_internal_notes_ticket_idx ON feedback_internal_notes(ticket_id,created_at,id);
CREATE TABLE feedback_status_history (
 id uuid PRIMARY KEY, ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,
 actor_id uuid REFERENCES users(id) ON DELETE SET NULL, from_status text NOT NULL DEFAULT '',
 to_status text NOT NULL CHECK (to_status IN ('open','investigating','replied','resolved','dismissed','closed')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX feedback_status_history_ticket_idx ON feedback_status_history(ticket_id,created_at,id);
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
 human_reviewed_at timestamptz, human_action text NOT NULL DEFAULT '',
 human_reviewer_id uuid REFERENCES users(id) ON DELETE SET NULL, human_note text NOT NULL DEFAULT '',
 recheck_state text NOT NULL DEFAULT 'none' CHECK (recheck_state IN ('none','queued','in_review','done')),
 recheck_requested_at timestamptz, recheck_requested_by uuid REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX comment_moderation_queue_idx ON comment_moderation(recheck_state,recheck_requested_at) WHERE recheck_state IN ('queued','in_review');
CREATE TABLE user_messages (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 kind text NOT NULL CHECK (kind IN ('review_result','moderation','comment_reply','report_result','admin_message','account','announcement')),
 -- event and data are the machine-readable pair the client routes on. title
 -- and body stay because released clients render them directly.
 event text NOT NULL DEFAULT '', data jsonb NOT NULL DEFAULT '{}',
 title text NOT NULL, body text NOT NULL, ref text NOT NULL DEFAULT '', read_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '90 days'
);
CREATE INDEX user_messages_user_idx ON user_messages(user_id,created_at DESC);
CREATE INDEX user_messages_event_idx ON user_messages(event,created_at DESC) WHERE event<>'';
CREATE TABLE announcements (
 id uuid PRIMARY KEY, title text NOT NULL, body text NOT NULL, published_at timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES users(id) ON DELETE SET NULL
);
CREATE TABLE app_releases (
 id uuid PRIMARY KEY, version text NOT NULL, channel text NOT NULL DEFAULT 'stable', platform text NOT NULL DEFAULT 'all', arch text NOT NULL DEFAULT 'all',
 minimum_version text NOT NULL DEFAULT '', notes_zh text NOT NULL DEFAULT '', notes_en text NOT NULL DEFAULT '', download_url text NOT NULL,
 published_at timestamptz NOT NULL DEFAULT now(), created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 enabled boolean NOT NULL DEFAULT true, revoked_at timestamptz, updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(version,channel,platform,arch)
);
CREATE INDEX app_releases_lookup_idx ON app_releases(channel,platform,arch,published_at DESC);
CREATE INDEX app_releases_lifecycle_lookup_idx ON app_releases(channel,platform,arch,published_at DESC) WHERE enabled AND revoked_at IS NULL;
CREATE TABLE user_coin_accounts (
 user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 balance_units bigint NOT NULL DEFAULT 0 CHECK (balance_units>=0), voting_frozen_at timestamptz,
 voting_frozen_reason text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE coin_ledger (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 delta_units bigint NOT NULL CHECK (delta_units<>0),
 kind text NOT NULL CHECK (kind IN ('checkin','resource_vote','creator_reward','admin_adjustment','reversal')),
 reference_type text NOT NULL DEFAULT '', reference_id text NOT NULL DEFAULT '', note text NOT NULL DEFAULT '',
 actor_id uuid REFERENCES users(id) ON DELETE SET NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX coin_ledger_user_idx ON coin_ledger(user_id,created_at DESC);
CREATE INDEX coin_ledger_reference_idx ON coin_ledger(reference_type,reference_id,created_at DESC);
CREATE TABLE daily_coin_checkins (
 user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE, checkin_date date NOT NULL,
 reward_units integer NOT NULL CHECK (reward_units BETWEEN 10 AND 50), ledger_id uuid NOT NULL UNIQUE REFERENCES coin_ledger(id),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,checkin_date)
);
CREATE TABLE resource_coin_votes (
 resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 coins smallint NOT NULL CHECK (coins BETWEEN 1 AND 2), creator_reward_units integer NOT NULL CHECK (creator_reward_units BETWEEN 1 AND 2),
 invalidated_at timestamptz, invalidated_by uuid REFERENCES users(id) ON DELETE SET NULL, invalidation_reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(resource_id,user_id)
);
CREATE INDEX resource_coin_votes_recent_idx ON resource_coin_votes(resource_id,created_at DESC) WHERE invalidated_at IS NULL;
