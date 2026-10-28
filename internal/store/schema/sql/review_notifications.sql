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
      COALESCE((SELECT artifact.package_version FROM revision_artifacts artifact WHERE artifact.revision_id=rrev.id AND artifact.package_version<>'' ORDER BY length(artifact.package_version) DESC,artifact.package_version DESC LIMIT 1),'')
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
        'collection_name', NEW.name,
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
