-- Project-scoped, durable identity conflicts from simulation and device ingress.
CREATE TABLE ztp_identity_issues (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 batch_ref varchar(160) NOT NULL,
 boot_channel varchar(8) NOT NULL CHECK (boot_channel IN ('239','143','sim')),
 identity_key varchar(200) NOT NULL,
 serial_number varchar(160) NOT NULL,
 source_ip varchar(45) NOT NULL DEFAULT '',
 reasons jsonb NOT NULL,
 event_count integer NOT NULL DEFAULT 1 CHECK(event_count>=1),
 first_seen_at timestamptz NOT NULL,
 last_seen_at timestamptz NOT NULL,
 last_trace_id varchar(32) NOT NULL,
 UNIQUE(project_id,batch_ref,boot_channel,identity_key)
);
CREATE INDEX ztp_identity_issues_recent ON ztp_identity_issues(project_id,last_seen_at DESC);
