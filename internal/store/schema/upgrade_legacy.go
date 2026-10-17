package schema

import (
	"context"
	"database/sql"
	"fmt"
)

// notificationTriggers is install-only. Upgrades keep their existing triggers,
// which pick up the replaced function bodies automatically, and add just the
// ones listed in notificationTriggersV6.
var notificationTriggers = `
CREATE TRIGGER resource_comments_reply_message AFTER INSERT ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_reply();
CREATE TRIGGER resource_comments_approved_reply_message AFTER UPDATE OF moderation_state ON resource_comments FOR EACH ROW WHEN (OLD.moderation_state='hidden' AND NEW.moderation_state='visible') EXECUTE FUNCTION notify_comment_reply();
CREATE TRIGGER resource_comments_hidden_message AFTER UPDATE OF moderation_state ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_hidden();
CREATE TRIGGER review_cases_result_message AFTER UPDATE OF state ON review_cases FOR EACH ROW EXECUTE FUNCTION notify_review_result();
CREATE TRIGGER resources_moderation_message AFTER UPDATE OF moderation_state ON resources FOR EACH ROW EXECUTE FUNCTION notify_resource_moderation();
CREATE TRIGGER feedback_report_result_message AFTER UPDATE OF status ON feedback_tickets FOR EACH ROW EXECUTE FUNCTION notify_report_result();
CREATE TRIGGER plugins_moderation_message AFTER UPDATE OF state ON plugins FOR EACH ROW EXECUTE FUNCTION notify_plugin_moderation();
` + notificationTriggersV6

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
