CREATE OR REPLACE FUNCTION notify_comment_reply() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_res_name text;
  v_author_name text;
BEGIN
  IF NEW.moderation_state = 'visible' AND (TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND OLD.moderation_state <> 'visible')) THEN
    SELECT COALESCE(
      (SELECT rrev.name FROM resource_revisions rrev WHERE rrev.resource_id = NEW.resource_id ORDER BY rrev.created_at DESC LIMIT 1),
      res.slug,
      NEW.resource_id::text
    ) INTO v_res_name
    FROM resources res WHERE res.id = NEW.resource_id;

    SELECT COALESCE(u.username, '') INTO v_author_name
    FROM users u WHERE u.id = NEW.user_id;

    IF NEW.parent_id IS NOT NULL THEN
      INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref)
      SELECT
        md5(random()::text || clock_timestamp()::text)::uuid,
        parent.user_id,
        'comment_reply',
        'comment.replied',
        jsonb_build_object(
          'comment_id', NEW.id::text,
          'parent_id', NEW.parent_id::text,
          'resource_id', NEW.resource_id::text,
          'resource_name', COALESCE(v_res_name, ''),
          'author_id', NEW.user_id::text,
          'author_name', COALESCE(v_author_name, ''),
          'body', NEW.body,
          'parent_body', LEFT(parent.body, 200)
        ),
        '', '', NEW.id::text
      FROM resource_comments parent
      WHERE parent.id = NEW.parent_id AND parent.user_id <> NEW.user_id;
    ELSE
      INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref)
      SELECT
        md5(random()::text || clock_timestamp()::text)::uuid,
        res.owner_id,
        'comment_reply',
        'comment.created',
        jsonb_build_object(
          'comment_id', NEW.id::text,
          'resource_id', NEW.resource_id::text,
          'resource_name', COALESCE(v_res_name, ''),
          'author_id', NEW.user_id::text,
          'author_name', COALESCE(v_author_name, ''),
          'body', NEW.body
        ),
        '', '', NEW.id::text
      FROM resources res
      WHERE res.id = NEW.resource_id AND res.owner_id <> NEW.user_id;
    END IF;
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_comment_hidden() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_res_name text;
  v_reason text;
  v_automated boolean;
  v_categories text[];
BEGIN
  IF NEW.moderation_state = 'hidden' AND (TG_OP = 'INSERT' OR (TG_OP = 'UPDATE' AND OLD.moderation_state <> 'hidden')) THEN
    SELECT COALESCE(
      (SELECT rrev.name FROM resource_revisions rrev WHERE rrev.resource_id = NEW.resource_id ORDER BY rrev.created_at DESC LIMIT 1),
      res.slug,
      NEW.resource_id::text
    ) INTO v_res_name
    FROM resources res WHERE res.id = NEW.resource_id;

    SELECT
      NULLIF(cm.reason, ''),
      COALESCE(cm.categories, '{}'::text[]),
      (cm.action = 'hide' AND cm.human_reviewed_at IS NULL)
    INTO v_reason, v_categories, v_automated
    FROM comment_moderation cm
    WHERE cm.comment_id = NEW.id;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      NEW.user_id,
      'moderation',
      'comment.hidden',
      jsonb_build_object(
        'comment_id', NEW.id::text,
        'resource_id', NEW.resource_id::text,
        'resource_name', COALESCE(v_res_name, ''),
        'comment_body', LEFT(NEW.body, 200),
        'reason', COALESCE(v_reason, '涉及违规内容'),
        'categories', to_jsonb(COALESCE(v_categories, '{}'::text[])),
        'automated', COALESCE(v_automated, TG_OP = 'INSERT')
      ),
      '', '',
      NEW.id::text
    );
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_review_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_owner_id uuid;
  v_res_id uuid;
  v_res_name text;
  v_version text;
  v_reviewer_name text;
  v_targets jsonb;
BEGIN
  IF OLD.state <> NEW.state AND NEW.state IN ('approved', 'rejected') THEN
    SELECT
      res.owner_id,
      res.id,
      COALESCE(NULLIF(rrev.name, ''), res.slug, res.id::text),
      rrev.version
    INTO v_owner_id, v_res_id, v_res_name, v_version
    FROM resource_revisions rrev
    JOIN resources res ON res.id = rrev.resource_id
    WHERE rrev.id = NEW.revision_id;

    SELECT COALESCE(u.username, '') INTO v_reviewer_name
    FROM users u WHERE u.id = NEW.reviewer_id;

    SELECT COALESCE(jsonb_agg(jsonb_build_object('target', p.target, 'state', p.state)), '[]'::jsonb)
    INTO v_targets
    FROM publications p
    WHERE p.revision_id = NEW.revision_id;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      v_owner_id,
      'review_result',
      CASE NEW.state WHEN 'approved' THEN 'review.approved' ELSE 'review.rejected' END,
      jsonb_build_object(
        'resource_id', v_res_id::text,
        'resource_name', COALESCE(v_res_name, ''),
        'version', COALESCE(v_version, ''),
        'revision_id', NEW.revision_id::text,
        'review_case_id', NEW.id::text,
        'state', NEW.state,
        'note', NEW.note,
        'reviewer_name', COALESCE(v_reviewer_name, ''),
        'targets', COALESCE(v_targets, '[]'::jsonb),
        'items', COALESCE(NEW.items, '[]'::jsonb)
      ),
      '', '',
      NEW.revision_id::text
    );
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_resource_moderation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_res_name text;
  v_operator_name text := '';
BEGIN
  IF OLD.moderation_state <> NEW.moderation_state AND (NEW.moderation_by = 'admin' OR OLD.moderation_by = 'admin') THEN
    SELECT COALESCE(
      (SELECT rrev.name FROM resource_revisions rrev WHERE rrev.resource_id = NEW.id ORDER BY rrev.created_at DESC LIMIT 1),
      NEW.slug,
      NEW.id::text
    ) INTO v_res_name;

    SELECT COALESCE(u.username, '') INTO v_operator_name
    FROM resource_events e
    JOIN users u ON u.id = e.actor_id
    WHERE e.resource_id = NEW.id
    ORDER BY e.created_at DESC, e.id DESC LIMIT 1;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      NEW.owner_id,
      'moderation',
      CASE NEW.moderation_state
        WHEN 'suspended' THEN 'resource.suspended'
        WHEN 'frozen' THEN 'resource.frozen'
        WHEN 'deleted' THEN 'resource.deleted'
        ELSE 'resource.restored'
      END,
      jsonb_build_object(
        'resource_id', NEW.id::text,
        'resource_name', COALESCE(v_res_name, ''),
        'state', NEW.moderation_state,
        'reason', COALESCE(NULLIF(NEW.moderation_reason, ''), OLD.moderation_reason, ''),
        'moderator_role', NEW.moderation_by,
        'moderator_name', COALESCE(v_operator_name, ''),
        'operator_name', COALESCE(v_operator_name, '')
      ),
      '', '',
      NEW.id::text
    );
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_report_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_target_name text := '';
  v_resource_id text := '';
  v_comment_id text := '';
  v_handler_name text := '';
BEGIN
  IF NEW.kind IN ('resource_report', 'comment_report') AND OLD.status <> NEW.status THEN
    IF lower(NEW.target_source) IN ('oronbox', 'resource') THEN
      v_resource_id := NEW.target_id;
      SELECT COALESCE(
        (SELECT rrev.name FROM resource_revisions rrev WHERE rrev.resource_id::text = NEW.target_id ORDER BY rrev.created_at DESC LIMIT 1),
        (SELECT res.slug FROM resources res WHERE res.id::text = NEW.target_id),
        NEW.target_id
      ) INTO v_target_name;
    ELSIF NEW.target_source = 'comment' THEN
      v_comment_id := NEW.target_id;
      SELECT
        COALESCE(rc.resource_id::text, ''),
        COALESCE(LEFT(rc.body, 100), '')
      INTO v_resource_id, v_target_name
      FROM resource_comments rc WHERE rc.id::text = NEW.target_id;
    END IF;

    SELECT COALESCE(u.username, '') INTO v_handler_name
    FROM feedback_status_history h
    JOIN users u ON u.id = h.actor_id
    WHERE h.ticket_id = NEW.id
    ORDER BY h.created_at DESC, h.id DESC LIMIT 1;

    IF v_handler_name = '' THEN
      SELECT COALESCE(u.username, '') INTO v_handler_name
      FROM feedback_replies r
      JOIN users u ON u.id = r.author_id
      WHERE r.ticket_id = NEW.id
      ORDER BY r.created_at DESC, r.id DESC LIMIT 1;
    END IF;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      NEW.user_id,
      'report_result',
      'report.updated',
      jsonb_build_object(
        'ticket_id', NEW.id::text,
        'ticket_subject', NEW.subject,
        'ticket_message', LEFT(NEW.message, 200),
        'status', NEW.status,
        'resolution', NEW.resolution,
        'handler_name', COALESCE(v_handler_name, ''),
        'operator_name', COALESCE(v_handler_name, ''),
        'target_source', NEW.target_source,
        'target_id', NEW.target_id,
        'target_name', COALESCE(v_target_name, ''),
        'resource_id', v_resource_id,
        'comment_id', v_comment_id
      ),
      '', '',
      NEW.id::text
    );
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_plugin_moderation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_version text;
BEGIN
  IF OLD.state <> NEW.state AND NEW.state <> 'pending' THEN
    SELECT COALESCE((SELECT pv.version FROM plugin_versions pv WHERE pv.plugin_id = NEW.id ORDER BY pv.created_at DESC LIMIT 1), '')
    INTO v_version;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      NEW.uploader_id,
      'moderation',
      CASE NEW.state
        WHEN 'listed' THEN CASE WHEN OLD.state = 'pending' THEN 'plugin.approved' ELSE 'plugin.relisted' END
        WHEN 'rejected' THEN 'plugin.rejected'
        ELSE 'plugin.delisted'
      END,
      jsonb_build_object(
        'plugin_id', NEW.id,
        'plugin_name', NEW.name,
        'version', COALESCE(v_version, ''),
        'state', NEW.state,
        'reason', NEW.moderation_reason
      ),
      '', '',
      NEW.id
    );
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_collection_review_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.state <> NEW.state AND NEW.state IN ('approved', 'rejected') THEN
    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref)
    SELECT
      md5(random()::text || clock_timestamp()::text)::uuid,
      collection.owner_id,
      'review_result',
      CASE NEW.state WHEN 'approved' THEN 'collection.approved' ELSE 'collection.rejected' END,
      jsonb_build_object(
        'collection_id', collection.id::text,
        'collection_name', collection.name,
        'revision_id', NEW.id::text,
        'state', NEW.state,
        'note', NEW.review_note
      ),
      '', '',
      NEW.id::text
    FROM resource_collections collection
    WHERE collection.id = NEW.collection_id;
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_coin_adjustment() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_operator_name text := '';
  v_reference_name text := '';
BEGIN
  IF NEW.kind IN ('admin_adjustment', 'reversal') THEN
    IF NEW.actor_id IS NOT NULL THEN
      SELECT COALESCE(u.username, '') INTO v_operator_name
      FROM users u WHERE u.id = NEW.actor_id;
    END IF;

    IF NEW.reference_type = 'resource' AND NEW.reference_id <> '' THEN
      SELECT COALESCE(
        (SELECT rrev.name FROM resource_revisions rrev WHERE rrev.resource_id::text = NEW.reference_id ORDER BY rrev.created_at DESC LIMIT 1),
        (SELECT res.slug FROM resources res WHERE res.id::text = NEW.reference_id),
        NEW.reference_id
      ) INTO v_reference_name;
    END IF;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      NEW.user_id,
      'account',
      CASE NEW.kind WHEN 'reversal' THEN 'coin.reversed' ELSE 'coin.adjusted' END,
      jsonb_build_object(
        'ledger_id', NEW.id::text,
        'delta_units', NEW.delta_units,
        'kind', NEW.kind,
        'reference_type', NEW.reference_type,
        'reference_id', NEW.reference_id,
        'reference_name', COALESCE(v_reference_name, ''),
        'operator_name', COALESCE(v_operator_name, ''),
        'reason', NEW.note
      ),
      '', '',
      NEW.id::text
    );
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_coin_vote_invalidated() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_res_name text := '';
  v_operator_name text := '';
BEGIN
  IF OLD.invalidated_at IS NULL AND NEW.invalidated_at IS NOT NULL THEN
    SELECT COALESCE(
      (SELECT rrev.name FROM resource_revisions rrev WHERE rrev.resource_id = NEW.resource_id ORDER BY rrev.created_at DESC LIMIT 1),
      (SELECT res.slug FROM resources res WHERE res.id = NEW.resource_id),
      NEW.resource_id::text
    ) INTO v_res_name;

    IF NEW.invalidated_by IS NOT NULL THEN
      SELECT COALESCE(u.username, '') INTO v_operator_name
      FROM users u WHERE u.id = NEW.invalidated_by;
    END IF;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      NEW.user_id,
      'account',
      'coin.vote_revoked',
      jsonb_build_object(
        'resource_id', NEW.resource_id::text,
        'resource_name', COALESCE(v_res_name, ''),
        'coins', NEW.coins,
        'reason', NEW.invalidation_reason,
        'operator_name', COALESCE(v_operator_name, '')
      ),
      '', '',
      NEW.resource_id::text
    );
  END IF;
  RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION notify_appeal_result() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  v_subject_name text := '';
  v_handler_name text := '';
BEGIN
  IF OLD.status <> NEW.status AND NEW.status IN ('upheld', 'overturned', 'dismissed') THEN
    IF NEW.subject_type = 'resource_review' THEN
      SELECT COALESCE(
        (SELECT rrev.name FROM resource_revisions rrev JOIN review_cases rc ON rc.revision_id = rrev.id WHERE rc.id::text = NEW.subject_id),
        NEW.subject_id
      ) INTO v_subject_name;
    ELSIF NEW.subject_type = 'resource_moderation' THEN
      SELECT COALESCE(
        (SELECT rrev.name FROM resource_revisions rrev WHERE rrev.resource_id::text = NEW.subject_id ORDER BY rrev.created_at DESC LIMIT 1),
        (SELECT res.slug FROM resources res WHERE res.id::text = NEW.subject_id),
        NEW.subject_id
      ) INTO v_subject_name;
    ELSIF NEW.subject_type = 'plugin_review' THEN
      SELECT COALESCE((SELECT p.name FROM plugins p WHERE p.id = NEW.subject_id), NEW.subject_id) INTO v_subject_name;
    ELSIF NEW.subject_type = 'collection_review' THEN
      SELECT COALESCE((SELECT c.name FROM resource_collections c WHERE c.id::text = NEW.subject_id), NEW.subject_id) INTO v_subject_name;
    ELSIF NEW.subject_type = 'comment_moderation' THEN
      SELECT COALESCE((SELECT LEFT(rc.body, 60) FROM resource_comments rc WHERE rc.id::text = NEW.subject_id), NEW.subject_id) INTO v_subject_name;
    ELSE
      v_subject_name := NEW.subject_type;
    END IF;

    IF NEW.handled_by IS NOT NULL THEN
      SELECT COALESCE(u.username, '') INTO v_handler_name
      FROM users u WHERE u.id = NEW.handled_by;
    END IF;

    INSERT INTO user_messages(id, user_id, kind, event, data, title, body, ref) VALUES(
      md5(random()::text || clock_timestamp()::text)::uuid,
      NEW.user_id,
      'report_result',
      'appeal.' || NEW.status,
      jsonb_build_object(
        'appeal_id', NEW.id::text,
        'subject_type', NEW.subject_type,
        'subject_id', NEW.subject_id,
        'subject_name', COALESCE(v_subject_name, ''),
        'appeal_message', LEFT(NEW.message, 200),
        'status', NEW.status,
        'resolution', NEW.resolution,
        'handler_name', COALESCE(v_handler_name, '')
      ),
      '', '',
      NEW.id::text
    );
  END IF;
  RETURN NEW;
END $$;
