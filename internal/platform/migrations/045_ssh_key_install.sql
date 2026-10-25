-- Public installation identity frozen under the original independent approval.
ALTER TABLE ssh_trust_requests ADD COLUMN install_key_snapshot jsonb NOT NULL DEFAULT 'null'::jsonb
 CHECK(jsonb_typeof(install_key_snapshot) IN('null','object') AND octet_length(install_key_snapshot::text)<=2048);
CREATE FUNCTION protect_ssh_install_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.install_key_snapshot IS DISTINCT FROM OLD.install_key_snapshot THEN
  RAISE EXCEPTION 'approved SSH installation identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_ssh_install_snapshot BEFORE UPDATE ON ssh_trust_requests FOR EACH ROW EXECUTE FUNCTION protect_ssh_install_snapshot();
