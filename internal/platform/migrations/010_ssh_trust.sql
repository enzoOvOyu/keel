-- SSH trust state on machines (WP-1.5, FR-1.2). Trust is per machine:
-- unverified until the first successful verification (PXE callback or a
-- restricted trust.bootstrap), established afterwards, revocable on exit.
-- The first host key fingerprint is recorded once and never overwritten
-- except by an audited trust.repair. Every transition lands in trust_logs.
-- Rollback: DROP TABLE trust_logs; ALTER TABLE machines DROP COLUMN
-- host_key_fingerprint, DROP COLUMN ssh_trust, DROP COLUMN trust_updated_at.
ALTER TABLE machines ADD COLUMN ssh_trust varchar NOT NULL DEFAULT 'unverified' CHECK(ssh_trust IN('unverified','established','revoked'));
ALTER TABLE machines ADD COLUMN host_key_fingerprint varchar(120) NOT NULL DEFAULT '';
ALTER TABLE machines ADD COLUMN trust_updated_at timestamptz NOT NULL DEFAULT '1970-01-01T00:00:00Z';
CREATE TABLE trust_logs (
 id varchar(32) PRIMARY KEY,
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 action varchar NOT NULL CHECK(action IN('trust.bootstrap','trust.verify','trust.repair','trust.revoke')),
 outcome varchar NOT NULL CHECK(outcome IN('success','failure')),
 fingerprint varchar(120) NOT NULL DEFAULT '',
 detail jsonb NOT NULL DEFAULT '{}',
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL
);
CREATE INDEX trust_logs_machine ON trust_logs(machine_id, created_at DESC);
CREATE INDEX trust_logs_project ON trust_logs(project_id, created_at DESC);
