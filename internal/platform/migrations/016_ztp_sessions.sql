-- WP-1.17: device/simulation sessions share an append-only nine-stage timeline.
-- Simulated managed is never projected to switches or FabricReadiness.
CREATE TABLE ztp_sessions (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 switch_id varchar(32) REFERENCES switches(id),
 batch_ref varchar(160) NOT NULL,
 mode varchar(12) NOT NULL CHECK(mode IN('sim','device')),
 boot_channel varchar(8) NOT NULL CHECK(boot_channel IN('239','143','60-43')),
 state varchar(20) NOT NULL CHECK(state IN('powered','dhcp','booturl','identity','lookup','fetch','config','restart','managed','isolated','conflict','failed')),
 state_version bigint NOT NULL CHECK(state_version>=1),
 attempt integer NOT NULL CHECK(attempt>=1),
 retry_max integer NOT NULL CHECK(retry_max BETWEEN 0 AND 10),
 identity_claim jsonb NOT NULL,
 identity_resolution jsonb NOT NULL,
 failure_phenomenon text,
 failure_cause text,
 failure_next_action text,
 next_retry_at timestamptz,
 created_by varchar(32) REFERENCES users(id),
 started_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 managed_at timestamptz,
 CHECK(mode<>'sim' OR created_by IS NOT NULL)
);
CREATE INDEX ztp_sessions_project ON ztp_sessions(project_id,started_at DESC);
CREATE INDEX ztp_sessions_switch ON ztp_sessions(switch_id,started_at DESC);
CREATE TABLE ztp_session_events (
 id varchar(32) PRIMARY KEY,
 session_id varchar(32) NOT NULL REFERENCES ztp_sessions(id),
 seq bigint NOT NULL CHECK(seq>=1),
 from_state varchar(20),
 to_state varchar(20) NOT NULL,
 event_code varchar(80) NOT NULL,
 evidence_ref varchar(300),
 attempt integer NOT NULL,
 occurred_at timestamptz NOT NULL,
 UNIQUE(session_id,seq)
);
