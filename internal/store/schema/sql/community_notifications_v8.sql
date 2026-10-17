
CREATE OR REPLACE FUNCTION notify_comment_reply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.moderation_state='visible' AND NEW.deleted_at IS NULL AND NEW.notified_at IS NULL
 AND EXISTS(SELECT 1 FROM resources WHERE id=NEW.resource_id AND moderation_state='visible')
 AND (NEW.parent_id IS NULL OR EXISTS(SELECT 1 FROM resource_comments WHERE id=NEW.parent_id AND moderation_state='visible' AND deleted_at IS NULL)) THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref)
  SELECT gen_random_uuid(),recipient,'comment_reply',CASE WHEN NEW.parent_id IS NULL THEN 'comment.created' ELSE 'comment.replied' END,
   jsonb_build_object('comment_id',NEW.id::text,'root_comment_id',COALESCE(NEW.parent_id,NEW.id)::text,'reply_to_id',NEW.reply_to_id::text,'resource_id',NEW.resource_id::text,'body',NEW.body),
   '','',NEW.id::text
  FROM (SELECT owner_id AS recipient FROM resources WHERE id=NEW.resource_id
        UNION SELECT user_id FROM resource_comments WHERE id=NEW.parent_id
        UNION SELECT user_id FROM resource_comments WHERE id=NEW.reply_to_id) recipients
  WHERE recipient<>NEW.user_id;
  UPDATE resource_comments SET notified_at=now() WHERE id=NEW.id AND notified_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
