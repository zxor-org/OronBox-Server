
ALTER TABLE resources ADD COLUMN edit_version bigint NOT NULL DEFAULT 0;
ALTER TABLE resource_revisions ADD COLUMN description text NOT NULL DEFAULT '', ADD COLUMN description_format text NOT NULL DEFAULT 'plain' CHECK(description_format IN ('plain','markdown'));
ALTER TABLE publications ADD COLUMN approved_snapshot jsonb NOT NULL DEFAULT '{}';
UPDATE publications p SET approved_snapshot=jsonb_build_object('name',r.name,'summary',r.summary,'description',r.summary,'description_format','plain') FROM resource_revisions r WHERE r.id=p.revision_id AND r.state IN ('approved','superseded');
UPDATE resource_revisions SET description=summary,summary='';
ALTER TABLE external_bindings ADD COLUMN verified_at timestamptz, ADD COLUMN verified_by uuid REFERENCES users(id) ON DELETE SET NULL;
