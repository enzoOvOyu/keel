-- Owner-only storage maintenance. Runtime signing identities never change.
CREATE TABLE credential_master_state (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 active_key_id char(64) NOT NULL CHECK(active_key_id ~ '^[0-9a-f]{64}$'),
 epoch bigint NOT NULL CHECK(epoch>0),
 updated_at timestamptz NOT NULL
);
CREATE TABLE credential_master_rewraps (
 id char(32) PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 source_key_id char(64) NOT NULL CHECK(source_key_id ~ '^[0-9a-f]{64}$'),
 target_key_id char(64) NOT NULL CHECK(target_key_id ~ '^[0-9a-f]{64}$' AND target_key_id<>source_key_id),
 key_count integer NOT NULL CHECK(key_count BETWEEN 0 AND 10000),
 epoch bigint NOT NULL CHECK(epoch>0),
 reason varchar(500) NOT NULL CHECK(length(reason)>0),
 created_at timestamptz NOT NULL
);
CREATE TABLE credential_master_rewrap_entries (
 rotation_id char(32) NOT NULL REFERENCES credential_master_rewraps(id),
 domain_key_id varchar(32) NOT NULL REFERENCES ssh_domain_keys(id),
 previous_cipher_hash char(64) NOT NULL CHECK(previous_cipher_hash ~ '^[0-9a-f]{64}$'),
 next_cipher_hash char(64) NOT NULL CHECK(next_cipher_hash ~ '^[0-9a-f]{64}$' AND next_cipher_hash<>previous_cipher_hash),
 PRIMARY KEY(rotation_id,domain_key_id)
);
CREATE TRIGGER immutable_master_rewrap BEFORE UPDATE OR DELETE ON credential_master_rewraps FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER immutable_master_rewrap_entry BEFORE UPDATE OR DELETE ON credential_master_rewrap_entries FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();

CREATE OR REPLACE FUNCTION protect_ssh_domain_key() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'SSH domain key evidence cannot be deleted'; END IF;
 IF NEW.ciphertext IS DISTINCT FROM OLD.ciphertext THEN
  IF ROW(NEW.id,NEW.project_id,NEW.credential_ref,NEW.credential_version,NEW.public_key,NEW.created_at,NEW.expires_at,NEW.revoked_at,NEW.revocation_reason,NEW.state_version)
   IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.credential_ref,OLD.credential_version,OLD.public_key,OLD.created_at,OLD.expires_at,OLD.revoked_at,OLD.revocation_reason,OLD.state_version)
   OR NOT EXISTS(SELECT 1 FROM credential_master_rewrap_entries r
    WHERE r.domain_key_id=OLD.id AND r.previous_cipher_hash=encode(sha256(OLD.ciphertext),'hex')
    AND r.next_cipher_hash=encode(sha256(NEW.ciphertext),'hex') AND r.xmin=pg_current_xact_id()::xid) THEN
   RAISE EXCEPTION 'SSH ciphertext changes require a matching owner rewrap entry in this transaction';
  END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.credential_ref,NEW.credential_version,NEW.public_key,NEW.created_at,NEW.expires_at)
  IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.credential_ref,OLD.credential_version,OLD.public_key,OLD.created_at,OLD.expires_at)
  OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR NEW.state_version<>OLD.state_version+1 THEN
  RAISE EXCEPTION 'SSH domain key versions are immutable; only first revocation is permitted';
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION protect_credential_master_state() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Master wrapping generation cannot be deleted'; END IF;
 IF NEW.singleton IS DISTINCT FROM OLD.singleton OR NEW.epoch<>OLD.epoch+1 OR NEW.active_key_id=OLD.active_key_id
 OR NOT EXISTS(SELECT 1 FROM credential_master_rewraps r WHERE r.source_key_id=OLD.active_key_id AND r.target_key_id=NEW.active_key_id AND r.epoch=NEW.epoch AND r.xmin=pg_current_xact_id()::xid) THEN
  RAISE EXCEPTION 'Master wrapping generation requires a matching owner rotation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_master_generation BEFORE UPDATE OR DELETE ON credential_master_state FOR EACH ROW EXECUTE FUNCTION protect_credential_master_state();
