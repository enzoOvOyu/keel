-- Explicit lineage declarations do not rewrite physical identity or authorize
-- credentials, DHCP reservations or device configuration.
CREATE TABLE ztp_identity_history (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 kind varchar(12) NOT NULL CHECK(kind IN('reinstall','rma')),
 from_session_id varchar(32) NOT NULL REFERENCES ztp_sessions(id),
 to_session_id varchar(32) NOT NULL REFERENCES ztp_sessions(id),
 from_switch_id varchar(32) NOT NULL REFERENCES switches(id),
 to_switch_id varchar(32) NOT NULL REFERENCES switches(id),
 mode varchar(12) NOT NULL CHECK(mode IN('sim','device')),
 source_snapshot jsonb NOT NULL,
 target_snapshot jsonb NOT NULL,
 reason varchar(1000) NOT NULL,
 request_hash varchar(64) NOT NULL,
 recorded_by varchar(32) NOT NULL REFERENCES users(id),
 recorded_at timestamptz NOT NULL,
 UNIQUE(project_id,to_session_id),
 CHECK(from_session_id<>to_session_id),
 CHECK((kind='reinstall' AND from_switch_id=to_switch_id) OR (kind='rma' AND from_switch_id<>to_switch_id))
);
CREATE INDEX ztp_identity_history_project ON ztp_identity_history(project_id,recorded_at DESC,id DESC);
CREATE TRIGGER ztp_identity_history_immutable BEFORE UPDATE OR DELETE ON ztp_identity_history
 FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
