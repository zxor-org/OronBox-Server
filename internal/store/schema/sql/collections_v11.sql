
ALTER TABLE resource_collections ADD COLUMN edit_version bigint NOT NULL DEFAULT 0;
ALTER TABLE resource_collection_revisions ADD COLUMN description text NOT NULL DEFAULT '',ADD COLUMN icon_sha256 text REFERENCES blobs(sha256),ADD COLUMN cover_sha256 text REFERENCES blobs(sha256);
CREATE TABLE creator_media_uploads(owner_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,blob_sha256 text NOT NULL REFERENCES blobs(sha256),created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(owner_id,blob_sha256));
