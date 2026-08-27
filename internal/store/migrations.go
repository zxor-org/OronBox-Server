package store

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion int64 = 6

// governanceSchema holds the moderation tables introduced in schema v6. It is
// shared by the fresh install and the upgrade path so the two cannot drift.
const governanceSchema = `
CREATE TABLE review_case_events (
 id uuid PRIMARY KEY, case_id uuid NOT NULL REFERENCES review_cases(id) ON DELETE CASCADE,
 actor_id uuid REFERENCES users(id) ON DELETE SET NULL,
 event text NOT NULL CHECK (event IN ('assigned','unassigned','checklist_saved','approved','rejected','reopened','priority_changed','appeal_opened','appeal_resolved','note')),
 note text NOT NULL DEFAULT '', checklist jsonb NOT NULL DEFAULT '[]', detail jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_case_events_case_idx ON review_case_events(case_id,created_at,id);
CREATE INDEX review_case_events_actor_idx ON review_case_events(actor_id,created_at DESC);
CREATE FUNCTION prevent_review_case_event_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'review case history is immutable'; END $$;
CREATE TRIGGER review_case_events_immutable BEFORE UPDATE ON review_case_events FOR EACH ROW EXECUTE FUNCTION prevent_review_case_event_update();
CREATE TABLE review_appeals (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 subject_type text NOT NULL CHECK (subject_type IN ('resource_review','resource_moderation','comment_moderation','plugin_review','collection_review','account')),
 subject_id text NOT NULL CHECK (subject_id<>''),
 review_case_id uuid REFERENCES review_cases(id) ON DELETE SET NULL,
 plugin_version_id uuid REFERENCES plugin_versions(id) ON DELETE SET NULL,
 comment_id uuid REFERENCES resource_comments(id) ON DELETE SET NULL,
 message text NOT NULL CHECK (char_length(message) BETWEEN 1 AND 4000),
 status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','reviewing','upheld','overturned','dismissed')),
 resolution text NOT NULL DEFAULT '',
 handled_by uuid REFERENCES users(id) ON DELETE SET NULL, handled_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX review_appeals_queue_idx ON review_appeals(status,created_at,id);
CREATE INDEX review_appeals_user_idx ON review_appeals(user_id,created_at DESC);
-- One open appeal per decision. Without this a creator can reopen the same
-- dispute repeatedly and bury the queue.
CREATE UNIQUE INDEX review_appeals_open_unique_idx ON review_appeals(subject_type,subject_id) WHERE status IN ('open','reviewing');
CREATE TABLE moderation_reason_templates (
 id uuid PRIMARY KEY,
 scope text NOT NULL CHECK (scope IN ('resource_review','comment','plugin','collection','report','account')),
 decision text NOT NULL DEFAULT '' CHECK (decision IN ('','approve','reject','hide','suspend','dismiss')),
 title text NOT NULL CHECK (title<>''), body text NOT NULL CHECK (body<>''),
 position integer NOT NULL DEFAULT 0, enabled boolean NOT NULL DEFAULT true,
 created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(scope,title)
);
CREATE INDEX moderation_reason_templates_pick_idx ON moderation_reason_templates(scope,decision,position,id) WHERE enabled;
`

// reasonTemplateSeed gives the review console a usable starter library instead
// of an empty selector on the first deploy.
const reasonTemplateSeed = `
INSERT INTO moderation_reason_templates(id,scope,decision,title,body,position) VALUES
 (md5('tpl-resource-assets')::uuid,'resource_review','reject','素材不完整','截图或图标缺失、模糊，或与实际界面不一致，请补充清晰的预览素材后重新提交。',10),
 (md5('tpl-resource-metadata')::uuid,'resource_review','reject','信息填写不规范','名称、简介或分类与资源实际内容不符，请按规范修改后重新提交。',20),
 (md5('tpl-resource-package')::uuid,'resource_review','reject','安装包无法验证','安装包无法在目标设备上正常安装或运行，请自测通过后重新提交。',30),
 (md5('tpl-resource-copyright')::uuid,'resource_review','reject','版权与授权存疑','未能确认素材或代码的授权来源，请补充授权说明。',40),
 (md5('tpl-comment-abuse')::uuid,'comment','hide','人身攻击或辱骂','评论包含攻击性言论，已按社区规范隐藏。',10),
 (md5('tpl-comment-spam')::uuid,'comment','hide','垃圾信息或引流','评论包含广告、外链引流或重复刷屏内容。',20),
 (md5('tpl-plugin-permission')::uuid,'plugin','reject','权限申请超出功能需要','插件申请的权限明显超出其描述的功能范围，请精简后重新提交。',10),
 (md5('tpl-collection-scope')::uuid,'collection','reject','合集内容不成主题','合集内资源缺乏一致主题，请调整后重新提交。',10),
 (md5('tpl-report-nonissue')::uuid,'report','dismiss','未发现违规','经核查未发现被举报内容违反社区规范。',10),
 (md5('tpl-report-handled')::uuid,'report','','已处理','举报属实，相关内容已按社区规范处理，感谢反馈。',20)
ON CONFLICT DO NOTHING;
`

// notificationFunctions is written with CREATE OR REPLACE so the upgrade path
// can install the same definitions the fresh schema uses. Every message now
// carries a stable machine-readable event name alongside the human copy, which
// is what lets the client route a notification without parsing Chinese text.
const notificationFunctions = `
CREATE OR REPLACE FUNCTION notify_comment_reply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.parent_id IS NOT NULL AND NEW.moderation_state='visible' THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref)
  SELECT md5(random()::text||clock_timestamp()::text)::uuid,parent.user_id,'comment_reply','comment.replied',
   jsonb_build_object('comment_id',NEW.id::text,'parent_id',NEW.parent_id::text,'resource_id',NEW.resource_id::text),
   '评论收到回复',NEW.body,NEW.id::text
  FROM resource_comments parent WHERE parent.id=NEW.parent_id AND parent.user_id<>NEW.user_id;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_comment_hidden() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Also fires on INSERT: automated moderation can store a comment as hidden
 -- from the start, and the author was never told about those.
 IF NEW.moderation_state='hidden' AND (TG_OP='INSERT' OR OLD.moderation_state<>NEW.moderation_state) THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'moderation','comment.hidden',
   jsonb_build_object('comment_id',NEW.id::text,'resource_id',NEW.resource_id::text,'automated',TG_OP='INSERT'),
   '评论已被隐藏',
   CASE WHEN TG_OP='INSERT' THEN '你的评论未通过内容审核，暂时不可见，可在消息中发起申诉' ELSE '你的评论经人工复审后已被隐藏' END,
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_review_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>NEW.state AND NEW.state IN ('approved','rejected') THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref)
  SELECT md5(random()::text||clock_timestamp()::text)::uuid,resource.owner_id,'review_result',
   CASE NEW.state WHEN 'approved' THEN 'review.approved' ELSE 'review.rejected' END,
   jsonb_build_object('resource_id',resource.id::text,'revision_id',NEW.revision_id::text,'review_case_id',NEW.id::text,'note',NEW.note),
   CASE NEW.state WHEN 'approved' THEN '资源审核已通过' ELSE '资源审核未通过' END,
   CASE NEW.state WHEN 'approved' THEN '你的资源版本已通过审核' ELSE '你的资源版本需要修改'||CASE WHEN NEW.note='' THEN '' ELSE '：'||NEW.note END END,
   NEW.revision_id::text
  FROM resource_revisions revision JOIN resources resource ON resource.id=revision.resource_id WHERE revision.id=NEW.revision_id;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_resource_moderation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.moderation_state<>NEW.moderation_state AND (NEW.moderation_by='admin' OR OLD.moderation_by='admin') THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.owner_id,'moderation',
   CASE NEW.moderation_state WHEN 'suspended' THEN 'resource.suspended' WHEN 'frozen' THEN 'resource.frozen' WHEN 'deleted' THEN 'resource.deleted' ELSE 'resource.restored' END,
   jsonb_build_object('resource_id',NEW.id::text,'state',NEW.moderation_state,'reason',COALESCE(NULLIF(NEW.moderation_reason,''),OLD.moderation_reason)),
   CASE NEW.moderation_state WHEN 'suspended' THEN '资源已下架' WHEN 'frozen' THEN '资源已冻结' ELSE '资源已恢复' END,
   CASE NEW.moderation_state WHEN 'suspended' THEN '资源已下架' WHEN 'frozen' THEN '资源已冻结' ELSE '资源已恢复' END||CASE WHEN COALESCE(NULLIF(NEW.moderation_reason,''),OLD.moderation_reason)='' THEN '' ELSE '：'||COALESCE(NULLIF(NEW.moderation_reason,''),OLD.moderation_reason) END,
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_report_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind IN ('resource_report','comment_report') AND OLD.status<>NEW.status THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'report_result','report.updated',
   jsonb_build_object('ticket_id',NEW.id::text,'status',NEW.status,'resolution',NEW.resolution),
   '举报处理结果',CASE WHEN NEW.resolution='' THEN '举报处理状态已更新为 '||NEW.status ELSE NEW.resolution END,NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_plugin_moderation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>NEW.state AND NEW.state<>'pending' THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.uploader_id,'moderation',
   CASE NEW.state WHEN 'listed' THEN CASE WHEN OLD.state='pending' THEN 'plugin.approved' ELSE 'plugin.relisted' END WHEN 'rejected' THEN 'plugin.rejected' ELSE 'plugin.delisted' END,
   jsonb_build_object('plugin_id',NEW.id,'state',NEW.state,'reason',NEW.moderation_reason),
   CASE NEW.state WHEN 'listed' THEN CASE WHEN OLD.state='pending' THEN '插件审核已通过' ELSE '插件已恢复上架' END WHEN 'rejected' THEN '插件审核未通过' ELSE '插件已被下架' END,
   CASE NEW.state WHEN 'listed' THEN CASE WHEN OLD.state='pending' THEN '你上传的插件已通过审核并上架' ELSE '你上传的插件已恢复上架' END WHEN 'rejected' THEN '你上传的插件未通过审核' ELSE '你上传的插件已被管理员下架' END||CASE WHEN NEW.moderation_reason='' THEN '' ELSE '：'||NEW.moderation_reason END,
   NEW.id);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_collection_review_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>NEW.state AND NEW.state IN ('approved','rejected') THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref)
  SELECT md5(random()::text||clock_timestamp()::text)::uuid,collection.owner_id,'review_result',
   CASE NEW.state WHEN 'approved' THEN 'collection.approved' ELSE 'collection.rejected' END,
   jsonb_build_object('collection_id',collection.id::text,'revision_id',NEW.id::text,'note',NEW.review_note),
   CASE NEW.state WHEN 'approved' THEN '合集审核已通过' ELSE '合集审核未通过' END,
   CASE NEW.state WHEN 'approved' THEN '你的合集已通过审核' ELSE '你的合集需要修改'||CASE WHEN NEW.review_note='' THEN '' ELSE '：'||NEW.review_note END END,
   NEW.id::text
  FROM resource_collections collection WHERE collection.id=NEW.collection_id;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_coin_adjustment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind IN ('admin_adjustment','reversal') THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'account',
   CASE NEW.kind WHEN 'reversal' THEN 'coin.reversed' ELSE 'coin.adjusted' END,
   jsonb_build_object('ledger_id',NEW.id::text,'delta_units',NEW.delta_units,'kind',NEW.kind,'reference_type',NEW.reference_type,'reference_id',NEW.reference_id,'note',NEW.note),
   CASE WHEN NEW.kind='reversal' THEN '硬币记录已冲正' WHEN NEW.delta_units>0 THEN '硬币已到账' ELSE '硬币已扣除' END,
   CASE WHEN NEW.delta_units>0 THEN '账户增加了 '||NEW.delta_units||' 枚硬币' ELSE '账户扣除了 '||abs(NEW.delta_units)||' 枚硬币' END||CASE WHEN NEW.note='' THEN '' ELSE '：'||NEW.note END,
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_coin_vote_invalidated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.invalidated_at IS NULL AND NEW.invalidated_at IS NOT NULL THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'account','coin.vote_revoked',
   jsonb_build_object('resource_id',NEW.resource_id::text,'coins',NEW.coins,'reason',NEW.invalidation_reason),
   '投币已作废','你对该资源的投币已被管理员作废'||CASE WHEN NEW.invalidation_reason='' THEN '' ELSE '：'||NEW.invalidation_reason END,
   NEW.resource_id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_appeal_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status<>NEW.status AND NEW.status IN ('upheld','overturned','dismissed') THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'report_result','appeal.'||NEW.status,
   jsonb_build_object('appeal_id',NEW.id::text,'subject_type',NEW.subject_type,'subject_id',NEW.subject_id,'status',NEW.status,'resolution',NEW.resolution),
   CASE NEW.status WHEN 'overturned' THEN '申诉已受理' WHEN 'upheld' THEN '申诉未改变原结果' ELSE '申诉已驳回' END,
   CASE WHEN NEW.resolution<>'' THEN NEW.resolution WHEN NEW.status='overturned' THEN '管理员已撤销原处理结果' WHEN NEW.status='upheld' THEN '复核后维持原处理结果' ELSE '申诉未被受理' END,
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
`

// notificationTriggers is install-only. Upgrades keep their existing triggers,
// which pick up the replaced function bodies automatically, and add just the
// ones listed in notificationTriggersV6.
const notificationTriggers = `
CREATE TRIGGER resource_comments_reply_message AFTER INSERT ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_reply();
CREATE TRIGGER resource_comments_approved_reply_message AFTER UPDATE OF moderation_state ON resource_comments FOR EACH ROW WHEN (OLD.moderation_state='hidden' AND NEW.moderation_state='visible') EXECUTE FUNCTION notify_comment_reply();
CREATE TRIGGER resource_comments_hidden_message AFTER UPDATE OF moderation_state ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_hidden();
CREATE TRIGGER review_cases_result_message AFTER UPDATE OF state ON review_cases FOR EACH ROW EXECUTE FUNCTION notify_review_result();
CREATE TRIGGER resources_moderation_message AFTER UPDATE OF moderation_state ON resources FOR EACH ROW EXECUTE FUNCTION notify_resource_moderation();
CREATE TRIGGER feedback_report_result_message AFTER UPDATE OF status ON feedback_tickets FOR EACH ROW EXECUTE FUNCTION notify_report_result();
CREATE TRIGGER plugins_moderation_message AFTER UPDATE OF state ON plugins FOR EACH ROW EXECUTE FUNCTION notify_plugin_moderation();
` + notificationTriggersV6

const notificationTriggersV6 = `
CREATE TRIGGER resource_comments_hidden_insert_message AFTER INSERT ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_hidden();
CREATE TRIGGER collection_revisions_result_message AFTER UPDATE OF state ON resource_collection_revisions FOR EACH ROW EXECUTE FUNCTION notify_collection_review_result();
CREATE TRIGGER coin_ledger_adjustment_message AFTER INSERT ON coin_ledger FOR EACH ROW EXECUTE FUNCTION notify_coin_adjustment();
CREATE TRIGGER resource_coin_votes_invalidated_message AFTER UPDATE OF invalidated_at ON resource_coin_votes FOR EACH ROW EXECUTE FUNCTION notify_coin_vote_invalidated();
CREATE TRIGGER review_appeals_result_message AFTER UPDATE OF status ON review_appeals FOR EACH ROW EXECUTE FUNCTION notify_appeal_result();
`

const notificationSchema = notificationFunctions + notificationTriggers

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
`
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply PostgreSQL schema v%d: %w", schemaVersion, err)
	}
	if _, err := tx.ExecContext(ctx, governanceSchema); err != nil {
		return fmt.Errorf("install PostgreSQL moderation tables: %w", err)
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

func migrateSchemaV4(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_revisions ADD COLUMN purchase_link text NOT NULL DEFAULT '', ADD COLUMN purchase_price numeric(12,2), ADD COLUMN purchase_currency text NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add external purchase metadata: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(4)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v4: %w", err)
	}
	return nil
}

func migrateSchemaV5(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resources DROP CONSTRAINT resources_moderation_state_check; ALTER TABLE resources ADD CONSTRAINT resources_moderation_state_check CHECK (moderation_state IN ('visible','suspended','frozen','deleted'))`); err != nil {
		return fmt.Errorf("allow deleted resource state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(5)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v5: %w", err)
	}
	return nil
}

func migrateSchemaV6(ctx context.Context, tx *sql.Tx) error {
	// Additive on purpose. Released OronBox clients read title and body, so
	// those stay and the structured pair is added beside them.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE user_messages ADD COLUMN event text NOT NULL DEFAULT '', ADD COLUMN data jsonb NOT NULL DEFAULT '{}'; CREATE INDEX user_messages_event_idx ON user_messages(event,created_at DESC) WHERE event<>''`); err != nil {
		return fmt.Errorf("add structured message fields: %w", err)
	}
	// Existing rows predate the taxonomy, so they get a namespaced marker
	// rather than a guessed event a client would then mis-route.
	if _, err := tx.ExecContext(ctx, `UPDATE user_messages SET event='legacy.'||kind WHERE event=''`); err != nil {
		return fmt.Errorf("backfill message events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE review_cases ADD COLUMN priority smallint NOT NULL DEFAULT 0 CHECK (priority BETWEEN 0 AND 3), ADD COLUMN due_at timestamptz, ADD COLUMN first_submitted_at timestamptz`); err != nil {
		return fmt.Errorf("add review case scheduling: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE review_cases SET first_submitted_at=created_at WHERE first_submitted_at IS NULL; UPDATE review_cases SET due_at=created_at+interval '48 hours' WHERE due_at IS NULL`); err != nil {
		return fmt.Errorf("backfill review case scheduling: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE review_cases
 ALTER COLUMN first_submitted_at SET DEFAULT now(), ALTER COLUMN first_submitted_at SET NOT NULL,
 ALTER COLUMN due_at SET DEFAULT now()+interval '48 hours', ALTER COLUMN due_at SET NOT NULL`); err != nil {
		return fmt.Errorf("require review case scheduling: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX review_cases_queue_idx ON review_cases(state,priority DESC,due_at,created_at) WHERE state='pending'`); err != nil {
		return fmt.Errorf("index the review queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE comment_moderation ADD COLUMN human_reviewer_id uuid REFERENCES users(id) ON DELETE SET NULL, ADD COLUMN human_note text NOT NULL DEFAULT '', ADD COLUMN recheck_state text NOT NULL DEFAULT 'none' CHECK (recheck_state IN ('none','queued','in_review','done')), ADD COLUMN recheck_requested_at timestamptz, ADD COLUMN recheck_requested_by uuid REFERENCES users(id) ON DELETE SET NULL; CREATE INDEX comment_moderation_queue_idx ON comment_moderation(recheck_state,recheck_requested_at) WHERE recheck_state IN ('queued','in_review')`); err != nil {
		return fmt.Errorf("add comment recheck queue: %w", err)
	}
	if _, err := tx.ExecContext(ctx, governanceSchema); err != nil {
		return fmt.Errorf("create moderation tables: %w", err)
	}
	// Existing triggers keep their names and pick up the replaced bodies, so
	// only the genuinely new triggers are created here.
	if _, err := tx.ExecContext(ctx, notificationFunctions+notificationTriggersV6); err != nil {
		return fmt.Errorf("upgrade notification triggers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, reasonTemplateSeed); err != nil {
		return fmt.Errorf("seed moderation reason templates: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(6)`); err != nil {
		return fmt.Errorf("record PostgreSQL schema v6: %w", err)
	}
	return nil
}

func migrateRevisionProvenance(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_revisions
 ADD COLUMN created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 ADD COLUMN created_via text NOT NULL DEFAULT 'creator',
 ADD COLUMN base_revision_id uuid REFERENCES resource_revisions(id) ON DELETE SET NULL`); err != nil {
		return fmt.Errorf("add resource revision provenance: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_revisions ADD CONSTRAINT resource_revisions_created_via_check CHECK (created_via IN ('creator','admin','import'))`); err != nil {
		return fmt.Errorf("validate resource revision provenance: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE resource_revisions revision SET created_by=resource.owner_id FROM resources resource WHERE resource.id=revision.resource_id AND revision.created_by IS NULL`); err != nil {
		return fmt.Errorf("backfill resource revision creator: %w", err)
	}
	return nil
}

func migrateDeviceAdministration(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE devices ADD COLUMN enabled boolean NOT NULL DEFAULT true, ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now()`); err != nil {
		return fmt.Errorf("add device administration fields: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX devices_astrobox_id_unique_idx ON devices(astrobox_id) WHERE astrobox_id<>''`); err != nil {
		return fmt.Errorf("enforce unique AstroBox device ids: %w", err)
	}
	return nil
}

func migrateGovernanceAndReleases(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_revisions ADD COLUMN governance_source jsonb NOT NULL DEFAULT '{}', ADD COLUMN governance_collection_id uuid REFERENCES resource_collections(id) ON DELETE SET NULL, ADD COLUMN governance_collection_position integer NOT NULL DEFAULT 0`); err != nil {
		return fmt.Errorf("add revision governance snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE resource_revision_collaborators (revision_id uuid NOT NULL REFERENCES resource_revisions(id) ON DELETE CASCADE,user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,PRIMARY KEY(revision_id,user_id))`); err != nil {
		return fmt.Errorf("add revision collaborator snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE app_releases ADD COLUMN enabled boolean NOT NULL DEFAULT true, ADD COLUMN revoked_at timestamptz, ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now()`); err != nil {
		return fmt.Errorf("add release lifecycle: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX app_releases_lifecycle_lookup_idx ON app_releases(channel,platform,arch,published_at DESC) WHERE enabled AND revoked_at IS NULL`); err != nil {
		return fmt.Errorf("index active releases: %w", err)
	}
	return nil
}

func migratePluginVersions(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE plugin_versions (id uuid PRIMARY KEY,plugin_id text NOT NULL REFERENCES plugins(id) ON DELETE CASCADE,revision_no integer NOT NULL,version text NOT NULL,name text NOT NULL,author text NOT NULL DEFAULT '',description text NOT NULL DEFAULT '',runtime text NOT NULL CHECK (runtime IN ('js','wasm','hybrid')),permissions jsonb NOT NULL DEFAULT '[]',package_sha256 char(64) NOT NULL REFERENCES blobs(sha256),icon_sha256 char(64) REFERENCES blobs(sha256),state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','listed','rejected','superseded')),moderation_reason text NOT NULL DEFAULT '',created_by uuid REFERENCES users(id) ON DELETE SET NULL,created_via text NOT NULL DEFAULT 'uploader' CHECK (created_via IN ('uploader','admin','import')),base_version_id uuid REFERENCES plugin_versions(id) ON DELETE SET NULL,created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),UNIQUE(plugin_id,revision_no))`); err != nil {
		return fmt.Errorf("create plugin versions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE plugins ADD COLUMN current_version_id uuid REFERENCES plugin_versions(id) ON DELETE SET NULL,ADD COLUMN pending_version_id uuid REFERENCES plugin_versions(id) ON DELETE SET NULL`); err != nil {
		return fmt.Errorf("add plugin version pointers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO plugin_versions(id,plugin_id,revision_no,version,name,author,description,runtime,permissions,package_sha256,icon_sha256,state,moderation_reason,created_by,created_via,created_at,updated_at) SELECT md5(random()::text||clock_timestamp()::text||id)::uuid,id,1,version,name,author,description,runtime,permissions,package_sha256,icon_sha256,CASE WHEN state='listed' THEN 'listed' WHEN state='rejected' THEN 'rejected' ELSE 'pending' END,moderation_reason,uploader_id,'import',created_at,updated_at FROM plugins`); err != nil {
		return fmt.Errorf("backfill plugin versions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE plugins plugin SET current_version_id=version.id FROM plugin_versions version WHERE version.plugin_id=plugin.id AND plugin.state IN ('listed','delisted'); UPDATE plugins plugin SET pending_version_id=version.id FROM plugin_versions version WHERE version.plugin_id=plugin.id AND plugin.state IN ('pending','rejected')`); err != nil {
		return fmt.Errorf("backfill plugin pointers: %w", err)
	}
	return nil
}

func migrateFeedbackHistory(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE feedback_tickets ADD COLUMN target_snapshot jsonb NOT NULL DEFAULT '{}'`); err != nil {
		return fmt.Errorf("add feedback target snapshot: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE feedback_internal_notes (id uuid PRIMARY KEY,ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,author_id uuid REFERENCES users(id) ON DELETE SET NULL,message text NOT NULL,created_at timestamptz NOT NULL DEFAULT now()); CREATE INDEX feedback_internal_notes_ticket_idx ON feedback_internal_notes(ticket_id,created_at,id)`); err != nil {
		return fmt.Errorf("create feedback internal notes: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE feedback_status_history (id uuid PRIMARY KEY,ticket_id uuid NOT NULL REFERENCES feedback_tickets(id) ON DELETE CASCADE,actor_id uuid REFERENCES users(id) ON DELETE SET NULL,from_status text NOT NULL DEFAULT '',to_status text NOT NULL CHECK (to_status IN ('open','investigating','replied','resolved','dismissed','closed')),created_at timestamptz NOT NULL DEFAULT now()); CREATE INDEX feedback_status_history_ticket_idx ON feedback_status_history(ticket_id,created_at,id); INSERT INTO feedback_status_history(id,ticket_id,from_status,to_status,created_at) SELECT md5(random()::text||clock_timestamp()::text||id::text)::uuid,id,'',status,created_at FROM feedback_tickets`); err != nil {
		return fmt.Errorf("create feedback status history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE feedback_tickets ticket SET target_snapshot=jsonb_strip_nulls(jsonb_build_object('source',ticket.target_source,'id',ticket.target_id,'url',ticket.target_url,'kind',CASE WHEN lower(ticket.target_source)='comment' THEN 'comment' WHEN ticket.target_source='' OR lower(ticket.target_source)='oronbox' THEN 'resource' ELSE 'external' END,'title',CASE WHEN lower(ticket.target_source)='comment' THEN (SELECT left(comment.body,500) FROM resource_comments comment WHERE comment.id::text=ticket.target_id) WHEN ticket.target_source='' OR lower(ticket.target_source)='oronbox' THEN (SELECT COALESCE(revision.name,resource.draft_name) FROM resources resource LEFT JOIN resource_revisions revision ON revision.id=resource.current_revision_id WHERE resource.id::text=ticket.target_id) END,'owner',CASE WHEN lower(ticket.target_source)='comment' THEN (SELECT account.username FROM resource_comments comment JOIN users account ON account.id=comment.user_id WHERE comment.id::text=ticket.target_id) WHEN ticket.target_source='' OR lower(ticket.target_source)='oronbox' THEN (SELECT account.username FROM resources resource JOIN users account ON account.id=resource.owner_id WHERE resource.id::text=ticket.target_id) END,'state',CASE WHEN lower(ticket.target_source)='comment' THEN (SELECT comment.moderation_state FROM resource_comments comment WHERE comment.id::text=ticket.target_id) WHEN ticket.target_source='' OR lower(ticket.target_source)='oronbox' THEN (SELECT resource.moderation_state FROM resources resource WHERE resource.id::text=ticket.target_id) END)) WHERE ticket.target_id<>''`); err != nil {
		return fmt.Errorf("backfill feedback target snapshots: %w", err)
	}
	return nil
}

func migrateCollectionLifecycle(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE resource_collections ADD COLUMN enabled boolean NOT NULL DEFAULT true;
ALTER TABLE resource_collection_revisions
 ADD COLUMN enabled boolean NOT NULL DEFAULT true,
 ADD COLUMN representative_resource_id uuid REFERENCES resources(id) ON DELETE SET NULL,
 ADD COLUMN created_by uuid REFERENCES users(id) ON DELETE SET NULL,
 ADD COLUMN created_via text NOT NULL DEFAULT 'creator',
 ADD COLUMN base_revision_id uuid REFERENCES resource_collection_revisions(id) ON DELETE SET NULL;
ALTER TABLE resource_collection_revisions ADD CONSTRAINT resource_collection_revisions_created_via_check CHECK (created_via IN ('creator','admin','import'));
CREATE TABLE resource_collection_revision_members (
 id uuid PRIMARY KEY, revision_id uuid NOT NULL REFERENCES resource_collection_revisions(id) ON DELETE CASCADE,
 resource_id uuid REFERENCES resources(id) ON DELETE SET NULL, resource_slug text NOT NULL, resource_name text NOT NULL DEFAULT '',
 position integer NOT NULL CHECK (position>=0), UNIQUE(revision_id,resource_id), UNIQUE(revision_id,position)
);
UPDATE resource_collection_revisions revision SET enabled=collection.enabled,representative_resource_id=collection.representative_resource_id,created_by=collection.owner_id,created_via='import',base_revision_id=(SELECT previous.id FROM resource_collection_revisions previous WHERE previous.collection_id=revision.collection_id AND previous.revision_no<revision.revision_no ORDER BY previous.revision_no DESC LIMIT 1) FROM resource_collections collection WHERE collection.id=revision.collection_id;
INSERT INTO resource_collection_revision_members(id,revision_id,resource_id,resource_slug,resource_name,position)
SELECT md5(random()::text||clock_timestamp()::text||revision.id::text||resource.id::text)::uuid,revision.id,resource.id,resource.slug,COALESCE(current_revision.name,resource.draft_name),row_number() OVER (PARTITION BY revision.id ORDER BY resource.collection_position,resource.created_at,resource.id)-1
FROM resource_collection_revisions revision JOIN resource_collections collection ON collection.id=revision.collection_id JOIN resources resource ON resource.collection_id=collection.id
LEFT JOIN resource_revisions current_revision ON current_revision.id=resource.current_revision_id
WHERE revision.id=collection.current_revision_id OR revision.state='pending'`); err != nil {
		return fmt.Errorf("add immutable collection lifecycle revisions: %w", err)
	}
	return nil
}

func migratePublicationHistory(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE publications ADD COLUMN lease_token uuid,ADD COLUMN lease_expires_at timestamptz`); err != nil {
		return fmt.Errorf("add publication polling lease: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE publication_attempts (id bigserial PRIMARY KEY,publication_id uuid NOT NULL REFERENCES publications(id) ON DELETE CASCADE,attempt_number integer NOT NULL,phase text NOT NULL CHECK (phase IN ('execute','poll','admin')),event text NOT NULL,state_from text NOT NULL,state_to text NOT NULL,error_message text NOT NULL DEFAULT '',detail jsonb NOT NULL DEFAULT '{}',created_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create publication attempt history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX publication_attempts_history_idx ON publication_attempts(publication_id,id DESC)`); err != nil {
		return fmt.Errorf("index publication attempt history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE FUNCTION prevent_publication_attempt_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'publication attempt history is immutable'; END $$; CREATE TRIGGER publication_attempts_immutable BEFORE UPDATE OR DELETE ON publication_attempts FOR EACH ROW EXECUTE FUNCTION prevent_publication_attempt_update()`); err != nil {
		return fmt.Errorf("protect immutable publication attempt history: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO publication_attempts(publication_id,attempt_number,phase,event,state_from,state_to,error_message,detail,created_at) SELECT id,attempts,'admin','history_imported',state,state,error_message,jsonb_build_object('imported',true,'external_id',external_id,'external_url',external_url,'status_detail',status_detail),updated_at FROM publications`); err != nil {
		return fmt.Errorf("backfill publication attempt history: %w", err)
	}
	return nil
}

func migrateStructuredAudit(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE audit_logs
 ADD COLUMN before_data jsonb,
 ADD COLUMN after_data jsonb,
 ADD COLUMN target_data jsonb NOT NULL DEFAULT '{}';
CREATE INDEX audit_logs_target_idx ON audit_logs((target_data->>'type'),(target_data->>'id'),id DESC) WHERE target_data->>'id'<>''`); err != nil {
		return fmt.Errorf("add structured audit data: %w", err)
	}
	return nil
}
