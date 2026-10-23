-- Trusted read-only gateway receipts. A fixture receipt cannot create device managed.
CREATE TABLE ztp_manageability_checks (
 id varchar(32) PRIMARY KEY,
 observation_id varchar(32) NOT NULL REFERENCES ztp_device_observations(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 session_id varchar(32) REFERENCES ztp_sessions(id),
 facts_hash varchar(64) NOT NULL,
 probe_hash varchar(64) NOT NULL,
 credential_batch_ref varchar(120) NOT NULL,
 evidence_source varchar(24) NOT NULL CHECK(evidence_source IN('device','protocol_fixture','unknown')),
 code varchar(32) NOT NULL,
 request jsonb NOT NULL,
 proof jsonb,
 attempt integer NOT NULL CHECK(attempt BETWEEN 1 AND 11),
 next_retry_at timestamptz,
 checked_at timestamptz NOT NULL,
 UNIQUE(observation_id,facts_hash,attempt)
);
CREATE INDEX ztp_manageability_recent ON ztp_manageability_checks(project_id,checked_at DESC);
CREATE INDEX ztp_manageability_observation ON ztp_manageability_checks(observation_id,facts_hash,attempt DESC);
CREATE FUNCTION keel_ztp_manageability_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'ZTP manageability receipts are immutable'; END $$;
CREATE TRIGGER ztp_manageability_immutable BEFORE UPDATE OR DELETE ON ztp_manageability_checks FOR EACH ROW EXECUTE FUNCTION keel_ztp_manageability_immutable();
