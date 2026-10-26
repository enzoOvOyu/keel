-- Independent approval to restore only the immutable installation domain key.
-- No secrets, second execution, new device lease or implicit lease renewal.
CREATE TABLE provision_key_repairs (
 id varchar(32) PRIMARY KEY,
 installation_id varchar(32) NOT NULL REFERENCES provision_installations(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object' AND octet_length(snapshot::text)<=16384),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 approved_by varchar(32) REFERENCES users(id),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 state varchar NOT NULL CHECK(state IN('pending','approved','consumed','unknown','succeeded','revoked')),
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>0),
 CHECK(approved_by IS NULL OR approved_by<>created_by),
 CHECK(state NOT IN('approved','consumed','unknown','succeeded') OR approved_by IS NOT NULL)
);
CREATE UNIQUE INDEX provision_key_repair_one_active ON provision_key_repairs(installation_id) WHERE state IN('pending','approved','consumed','unknown');
CREATE INDEX provision_key_repairs_project ON provision_key_repairs(project_id,installation_id,created_at DESC,id);
CREATE FUNCTION protect_provision_key_repair() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'provision repair evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.installation_id,NEW.project_id,NEW.snapshot,NEW.created_by,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.installation_id,OLD.project_id,OLD.snapshot,OLD.created_by,OLD.created_at,OLD.expires_at)
 OR NEW.state_version<>OLD.state_version+1
 OR (OLD.approved_by IS NOT NULL AND NEW.approved_by IS DISTINCT FROM OLD.approved_by)
 OR NOT ((OLD.state='pending' AND NEW.state IN('approved','revoked'))
 OR (OLD.state='approved' AND NEW.state IN('consumed','revoked'))
 OR (OLD.state='consumed' AND NEW.state IN('unknown','succeeded'))
 OR (OLD.state='unknown' AND NEW.state='consumed')) THEN
  RAISE EXCEPTION 'provision repair snapshot or transition is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_provision_key_repair BEFORE UPDATE OR DELETE ON provision_key_repairs FOR EACH ROW EXECUTE FUNCTION protect_provision_key_repair();
