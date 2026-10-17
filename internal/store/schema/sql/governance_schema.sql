
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
