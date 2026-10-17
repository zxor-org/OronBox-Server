
CREATE OR REPLACE FUNCTION notify_comment_reply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.parent_id IS NOT NULL AND NEW.moderation_state='visible' THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref)
  SELECT md5(random()::text||clock_timestamp()::text)::uuid,parent.user_id,'comment_reply','comment.replied',
   jsonb_build_object('comment_id',NEW.id::text,'parent_id',NEW.parent_id::text,'resource_id',NEW.resource_id::text,'body',NEW.body),
   '','',NEW.id::text
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
   '','',
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
   '','',
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
   '','',
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION notify_report_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind IN ('resource_report','comment_report') AND OLD.status<>NEW.status THEN
  INSERT INTO user_messages(id,user_id,kind,event,data,title,body,ref) VALUES(
   md5(random()::text||clock_timestamp()::text)::uuid,NEW.user_id,'report_result','report.updated',
   jsonb_build_object(
    'ticket_id',NEW.id::text,'status',NEW.status,'resolution',NEW.resolution,
    'target_source',NEW.target_source,'target_id',NEW.target_id,
    'resource_id',CASE WHEN lower(NEW.target_source) IN ('oronbox','resource') THEN NEW.target_id WHEN NEW.target_source='comment' THEN COALESCE((SELECT resource_id::text FROM resource_comments WHERE id::text=NEW.target_id),'') ELSE '' END,
    'comment_id',CASE WHEN NEW.target_source='comment' THEN NEW.target_id ELSE '' END),
   '','',NEW.id::text);
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
   '','',
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
   '','',
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
   jsonb_build_object('ledger_id',NEW.id::text,'delta_units',NEW.delta_units,'kind',NEW.kind,'reference_type',NEW.reference_type,'reference_id',NEW.reference_id,'reason',NEW.note),
   '','',
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
   '','',
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
   '','',
   NEW.id::text);
 END IF;
 RETURN NEW;
END $$;
