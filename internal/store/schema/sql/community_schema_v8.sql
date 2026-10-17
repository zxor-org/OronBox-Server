
ALTER TABLE resource_comments ADD COLUMN reply_to_id uuid REFERENCES resource_comments(id) ON DELETE SET NULL;
ALTER TABLE resource_comments ADD COLUMN notified_at timestamptz;
UPDATE resource_comments SET reply_to_id=parent_id,notified_at=CASE WHEN moderation_state='visible' THEN now() END;
CREATE INDEX comments_thread_cursor_idx ON resource_comments(parent_id,created_at,id);
CREATE INDEX messages_user_cursor_idx ON user_messages(user_id,created_at DESC,id DESC);
