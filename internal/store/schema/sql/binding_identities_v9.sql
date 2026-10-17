
CREATE TABLE external_binding_identities (
 binding_id uuid NOT NULL REFERENCES external_bindings(id) ON DELETE CASCADE,
 resource_id uuid NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
 provider text NOT NULL, external_id text NOT NULL,
 PRIMARY KEY(provider,external_id)
);
CREATE FUNCTION index_external_binding_identities() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE remote_id text;
BEGIN
 DELETE FROM external_binding_identities WHERE binding_id=NEW.id;
 IF NEW.provider='bandbbs' AND left(trim(NEW.external_id),1)='{' THEN
  FOR remote_id IN SELECT DISTINCT value FROM jsonb_each_text(NEW.external_id::jsonb) LOOP
   INSERT INTO external_binding_identities VALUES(NEW.id,NEW.resource_id,NEW.provider,remote_id);
  END LOOP;
 ELSE
  INSERT INTO external_binding_identities VALUES(NEW.id,NEW.resource_id,NEW.provider,NEW.external_id);
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER external_binding_identity_index AFTER INSERT OR UPDATE OF external_id,provider,resource_id ON external_bindings FOR EACH ROW EXECUTE FUNCTION index_external_binding_identities();
UPDATE external_bindings SET external_id=external_id;
