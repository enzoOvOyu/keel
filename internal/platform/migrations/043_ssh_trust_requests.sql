-- Approved, single-target first-contact verification. No secret material is stored.
ALTER TABLE machines DROP CONSTRAINT machines_ssh_trust_check;
ALTER TABLE machines ADD CONSTRAINT machines_ssh_trust_check CHECK(ssh_trust IN('unverified','established','broken','revoked'));
-- Existing and simulated trust never silently authorize the real transport.
ALTER TABLE machines ADD COLUMN ssh_trust_source varchar NOT NULL DEFAULT 'legacy' CHECK(ssh_trust_source IN('legacy','sim','ssh'));
CREATE TABLE ssh_trust_requests (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 action varchar NOT NULL CHECK(action IN('trust.bootstrap','trust.repair')),
 asset_version bigint NOT NULL CHECK(asset_version>0),
 expected_trust varchar NOT NULL CHECK(expected_trust IN('unverified','broken','established')),
 host_key varchar(50) NOT NULL CHECK(host_key ~ '^SHA256:[A-Za-z0-9+/]{43}$'),
 config_hash varchar(64) NOT NULL CHECK(config_hash ~ '^[a-f0-9]{64}$'),
 credential_ref varchar(64) NOT NULL CHECK(length(credential_ref)>0),
 credential_version varchar(64) NOT NULL CHECK(length(credential_version)>0),
 credential_expires_at timestamptz NOT NULL,
 reason varchar(500) NOT NULL CHECK(length(reason)>0),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 approved_by varchar(32) REFERENCES users(id),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 state varchar NOT NULL DEFAULT 'pending' CHECK(state IN('pending','approved','consumed','succeeded','failed','unknown','revoked')),
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>0),
 execution_id varchar(32) UNIQUE REFERENCES exec_executions(id),
 CHECK(approved_by IS NULL OR approved_by<>created_by),
 CHECK(expires_at>=created_at+interval '5 minutes' AND expires_at<=created_at+interval '1 hour' AND expires_at<=credential_expires_at),
 CHECK((action='trust.bootstrap' AND expected_trust='unverified') OR (action='trust.repair' AND expected_trust IN('broken','established'))),
 CHECK(state NOT IN('approved','consumed','succeeded','failed','unknown') OR approved_by IS NOT NULL),
 CHECK((state IN('consumed','succeeded','failed','unknown'))=(execution_id IS NOT NULL))
);
CREATE INDEX ssh_trust_requests_project_created ON ssh_trust_requests(project_id,created_at DESC,id);
CREATE FUNCTION protect_ssh_trust_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'SSH trust authorization evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.machine_id,NEW.action,NEW.asset_version,NEW.expected_trust,NEW.host_key,NEW.config_hash,NEW.credential_ref,NEW.credential_version,NEW.credential_expires_at,NEW.reason,NEW.created_by,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.machine_id,OLD.action,OLD.asset_version,OLD.expected_trust,OLD.host_key,OLD.config_hash,OLD.credential_ref,OLD.credential_version,OLD.credential_expires_at,OLD.reason,OLD.created_by,OLD.created_at,OLD.expires_at)
 OR NEW.state_version<>OLD.state_version+1
 OR (OLD.approved_by IS NOT NULL AND NEW.approved_by IS DISTINCT FROM OLD.approved_by)
 OR (OLD.execution_id IS NOT NULL AND NEW.execution_id IS DISTINCT FROM OLD.execution_id) THEN
  RAISE EXCEPTION 'SSH trust authorization snapshot is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_ssh_trust_request BEFORE UPDATE OR DELETE ON ssh_trust_requests FOR EACH ROW EXECUTE FUNCTION protect_ssh_trust_request();
-- A credential reference/version is consumed at most once for its target,
-- including unknown or failed attempts; recovery queries reuse that attempt.
CREATE UNIQUE INDEX ssh_trust_credential_single_use ON ssh_trust_requests(project_id,machine_id,credential_ref,credential_version) WHERE execution_id IS NOT NULL;
