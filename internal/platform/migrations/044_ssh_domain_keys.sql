-- Control-plane domain keys: encrypted material only; runtime roles are read-only.
CREATE TABLE ssh_domain_keys (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 credential_ref varchar(64) NOT NULL CHECK(credential_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
 credential_version varchar(64) NOT NULL CHECK(credential_version ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
 public_key varchar(256) NOT NULL CHECK(public_key LIKE 'ssh-ed25519 %'),
 ciphertext bytea NOT NULL CHECK(octet_length(ciphertext) BETWEEN 64 AND 1024),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 revoked_at timestamptz,
 revocation_reason varchar(500) NOT NULL DEFAULT '',
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>0),
 UNIQUE(project_id,credential_ref,credential_version),
 CHECK(revoked_at IS NULL OR revoked_at>=created_at),
 CHECK((revoked_at IS NULL AND revocation_reason='') OR (revoked_at IS NOT NULL AND length(revocation_reason)>0))
);
CREATE INDEX ssh_domain_keys_project_created ON ssh_domain_keys(project_id,created_at DESC,id);
CREATE FUNCTION protect_ssh_domain_key() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'SSH domain key evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.credential_ref,NEW.credential_version,NEW.public_key,NEW.ciphertext,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.credential_ref,OLD.credential_version,OLD.public_key,OLD.ciphertext,OLD.created_at,OLD.expires_at)
 OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR NEW.state_version<>OLD.state_version+1 THEN
  RAISE EXCEPTION 'SSH domain key versions are immutable; only first revocation is permitted';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_ssh_domain_key BEFORE UPDATE OR DELETE ON ssh_domain_keys FOR EACH ROW EXECUTE FUNCTION protect_ssh_domain_key();
