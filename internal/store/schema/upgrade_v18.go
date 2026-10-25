package schema

import (
	"context"
	"database/sql"
)

const commentVisibilityV18 = `CREATE OR REPLACE FUNCTION notify_comment_hidden() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.moderation_state='hidden' AND OLD.moderation_state<>NEW.moderation_state THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'moderation','comment.hidden',
   jsonb_build_object('comment_id',NEW.id::text,'resource_id',NEW.resource_id::text,'automated',TG_OP='INSERT'),
   '','',
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
`

func migrateSchemaV18(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, commentVisibilityV18); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version)VALUES(18)`)
	return err
}
