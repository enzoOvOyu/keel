-- Actual installation proof permanently records the approved domain identity.
-- Legacy read-only proofs remain null; no fabricated backfill is permitted.
ALTER TABLE machines ADD COLUMN ssh_domain_credential jsonb NOT NULL DEFAULT 'null'::jsonb
 CHECK(jsonb_typeof(ssh_domain_credential) IN('null','object') AND octet_length(ssh_domain_credential::text)<=2048);
CREATE FUNCTION protect_ssh_domain_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.ssh_domain_credential <> 'null'::jsonb AND NEW.ssh_domain_credential = 'null'::jsonb THEN
  RAISE EXCEPTION 'proven SSH domain identity cannot be cleared';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER preserve_ssh_domain_binding BEFORE UPDATE ON machines FOR EACH ROW EXECUTE FUNCTION protect_ssh_domain_binding();
