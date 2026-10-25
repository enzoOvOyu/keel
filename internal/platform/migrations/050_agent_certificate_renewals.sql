-- The enrollment credential, its original key binding and all task snapshots
-- remain immutable. Renewal appends a leaf; revoking the credential revokes every
-- generation. No enrollment token, private key or new task queue is introduced.
CREATE TABLE agent_certificate_renewals (
 credential_id char(32) NOT NULL REFERENCES agent_credentials(id),
 predecessor_hash char(64) NOT NULL CHECK(predecessor_hash ~ '^[0-9a-f]{64}$'),
 certificate_hash char(64) NOT NULL UNIQUE CHECK(certificate_hash ~ '^[0-9a-f]{64}$'),
 certificate_pem text NOT NULL CHECK(octet_length(certificate_pem)<=8192),
 issued_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>issued_at AND expires_at<=issued_at+interval '1 hour'),
 PRIMARY KEY(credential_id,predecessor_hash)
);
CREATE INDEX agent_renewal_expiry ON agent_certificate_renewals(credential_id,expires_at DESC);
CREATE FUNCTION check_agent_renewal_predecessor() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE predecessor_expiry timestamptz;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM agent_credentials WHERE id=NEW.credential_id AND revoked_at IS NULL) THEN
  RAISE EXCEPTION 'Agent credential is unavailable';
 END IF;
 SELECT expires_at INTO predecessor_expiry FROM (
  SELECT expires_at FROM agent_credentials WHERE id=NEW.credential_id AND certificate_hash=NEW.predecessor_hash
  UNION ALL
  SELECT expires_at FROM agent_certificate_renewals WHERE credential_id=NEW.credential_id AND certificate_hash=NEW.predecessor_hash
 ) predecessor LIMIT 1;
 IF predecessor_expiry IS NULL OR NEW.issued_at>=predecessor_expiry
 OR NEW.issued_at<predecessor_expiry-interval '20 minutes'
 OR NEW.expires_at<=predecessor_expiry OR NEW.issued_at>now()+interval '1 second' THEN
  RAISE EXCEPTION 'Agent renewal requires an eligible registered predecessor';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER valid_agent_renewal BEFORE INSERT ON agent_certificate_renewals
 FOR EACH ROW EXECUTE FUNCTION check_agent_renewal_predecessor();
CREATE TRIGGER immutable_agent_renewal BEFORE UPDATE OR DELETE ON agent_certificate_renewals
 FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();

-- Heartbeat and task streams must use the same actual leaf even when two
-- generations of the same stable credential overlap during a safe handover.
ALTER TABLE agent_connections ADD COLUMN certificate_hash char(64);
UPDATE agent_connections c SET certificate_hash=a.certificate_hash FROM agent_credentials a WHERE a.id=c.credential_id;
ALTER TABLE agent_connections ALTER COLUMN certificate_hash SET NOT NULL;
ALTER TABLE agent_connections ADD CHECK(certificate_hash ~ '^[0-9a-f]{64}$');
CREATE FUNCTION protect_agent_connection_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(NEW.id,NEW.credential_id,NEW.boot_id,NEW.agent_version,NEW.connected_at,NEW.certificate_hash)
 IS DISTINCT FROM ROW(OLD.id,OLD.credential_id,OLD.boot_id,OLD.agent_version,OLD.connected_at,OLD.certificate_hash) THEN
  RAISE EXCEPTION 'Agent connection identity is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_agent_connection_identity BEFORE UPDATE ON agent_connections
 FOR EACH ROW EXECUTE FUNCTION protect_agent_connection_identity();
