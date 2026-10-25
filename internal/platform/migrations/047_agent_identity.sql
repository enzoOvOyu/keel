-- Bootstrap secrets and private keys never enter domain rows. Only SHA256
-- token digests and public, project-bound short-lived client certificates do.
CREATE TABLE agent_enrollments (
 id char(32) PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 project_id char(32) NOT NULL REFERENCES projects(id),
 machine_id char(32) NOT NULL REFERENCES machines(id),
 asset_version bigint NOT NULL CHECK(asset_version>0),
 serial_number varchar(128) NOT NULL,
 authority_id char(64) NOT NULL CHECK(authority_id ~ '^[0-9a-f]{64}$'),
 token_hash char(64) NOT NULL UNIQUE CHECK(token_hash ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at AND expires_at<=created_at+interval '15 minutes'),
 reason varchar(500) NOT NULL CHECK(length(reason)>0),
 consumed_at timestamptz,
 certificate_id char(32),
 revoked_at timestamptz,
 CHECK((consumed_at IS NULL)=(certificate_id IS NULL)),
 CHECK(consumed_at IS NULL OR (consumed_at>=created_at AND consumed_at<expires_at)),
 CHECK(revoked_at IS NULL OR (revoked_at>=created_at AND consumed_at IS NULL))
);
CREATE TABLE agent_credentials (
 id char(32) PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 enrollment_id char(32) NOT NULL UNIQUE REFERENCES agent_enrollments(id),
 project_id char(32) NOT NULL REFERENCES projects(id),
 machine_id char(32) NOT NULL REFERENCES machines(id),
 asset_version bigint NOT NULL CHECK(asset_version>0),
 serial_number varchar(128) NOT NULL,
 authority_id char(64) NOT NULL CHECK(authority_id ~ '^[0-9a-f]{64}$'),
 certificate_pem text NOT NULL CHECK(octet_length(certificate_pem)<=8192),
 certificate_hash char(64) NOT NULL UNIQUE CHECK(certificate_hash ~ '^[0-9a-f]{64}$'),
 agent_version varchar(64) NOT NULL,
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at AND expires_at<=created_at+interval '1 hour'),
 revoked_at timestamptz,
 revocation_reason varchar(500) NOT NULL DEFAULT '',
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>0),
 CHECK((revoked_at IS NULL AND revocation_reason='') OR (revoked_at>=created_at AND length(revocation_reason)>0))
);
CREATE UNIQUE INDEX agent_one_active_machine ON agent_credentials(machine_id) WHERE revoked_at IS NULL;
ALTER TABLE agent_enrollments ADD FOREIGN KEY(certificate_id) REFERENCES agent_credentials(id);
CREATE TABLE agent_connections (
 id char(32) PRIMARY KEY CHECK(id ~ '^[0-9a-f]{32}$'),
 credential_id char(32) NOT NULL REFERENCES agent_credentials(id),
 boot_id char(32) NOT NULL CHECK(boot_id ~ '^[0-9a-f]{32}$'),
 agent_version varchar(64) NOT NULL,
 sequence bigint NOT NULL DEFAULT 0 CHECK(sequence>=0),
 connected_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 disconnected_at timestamptz,
 CHECK(disconnected_at IS NULL OR disconnected_at>=connected_at)
);
CREATE UNIQUE INDEX agent_one_active_connection ON agent_connections(credential_id) WHERE disconnected_at IS NULL;
CREATE INDEX agent_connections_liveness ON agent_connections(credential_id,last_seen_at DESC);
CREATE FUNCTION protect_agent_enrollment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Agent enrollment evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.machine_id,NEW.asset_version,NEW.serial_number,NEW.authority_id,NEW.token_hash,NEW.created_at,NEW.expires_at,NEW.reason)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.machine_id,OLD.asset_version,OLD.serial_number,OLD.authority_id,OLD.token_hash,OLD.created_at,OLD.expires_at,OLD.reason)
 OR OLD.consumed_at IS NOT NULL OR OLD.revoked_at IS NOT NULL
 OR ((NEW.consumed_at IS NULL)=(NEW.revoked_at IS NULL)) THEN
  RAISE EXCEPTION 'Agent enrollment is immutable and single use';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_agent_enrollment BEFORE UPDATE OR DELETE ON agent_enrollments FOR EACH ROW EXECUTE FUNCTION protect_agent_enrollment();
CREATE FUNCTION protect_agent_credential() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Agent certificate evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.enrollment_id,NEW.project_id,NEW.machine_id,NEW.asset_version,NEW.serial_number,NEW.authority_id,NEW.certificate_pem,NEW.certificate_hash,NEW.agent_version,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.enrollment_id,OLD.project_id,OLD.machine_id,OLD.asset_version,OLD.serial_number,OLD.authority_id,OLD.certificate_pem,OLD.certificate_hash,OLD.agent_version,OLD.created_at,OLD.expires_at)
 OR OLD.revoked_at IS NOT NULL OR NEW.revoked_at IS NULL OR NEW.state_version<>OLD.state_version+1 THEN
  RAISE EXCEPTION 'Agent credentials are immutable except first revocation';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_agent_credential BEFORE UPDATE OR DELETE ON agent_credentials FOR EACH ROW EXECUTE FUNCTION protect_agent_credential();
