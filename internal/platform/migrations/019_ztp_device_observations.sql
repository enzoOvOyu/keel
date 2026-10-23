-- RFC 8572 device observations are evidence, not a verified managed state.
-- The API creates rows only after direct IDevID, source and active-route checks.
CREATE TABLE ztp_device_observations (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 batch_ref varchar(160) NOT NULL,
 switch_id varchar(32) REFERENCES switches(id),
 serial_number varchar(160) NOT NULL,
 source_ip varchar(45) NOT NULL,
 boot_channel varchar(8) NOT NULL CHECK(boot_channel='143'),
 event_type varchar(20) NOT NULL CHECK(event_type IN('data-served','report')),
 progress_type varchar(60),
 message text,
 offer_hash varchar(64) NOT NULL,
 evidence_sha256 varchar(64),
 trace_id varchar(32) NOT NULL,
 occurred_at timestamptz NOT NULL,
 CHECK ((event_type='data-served' AND progress_type IS NULL AND evidence_sha256 IS NOT NULL)
     OR (event_type='report' AND progress_type IS NOT NULL)),
 CHECK (message IS NULL OR length(message)<=1000)
);
CREATE INDEX ztp_device_observations_project ON ztp_device_observations(project_id,occurred_at DESC);
CREATE INDEX ztp_device_observations_identity ON ztp_device_observations(project_id,batch_ref,serial_number,occurred_at DESC);

CREATE FUNCTION keel_ztp_observation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'ZTP device observations are append-only'; END $$;
CREATE TRIGGER ztp_device_observations_immutable BEFORE UPDATE OR DELETE ON ztp_device_observations
 FOR EACH ROW EXECUTE FUNCTION keel_ztp_observation_immutable();
