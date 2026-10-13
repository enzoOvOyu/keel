-- LLDP snapshots and line-order differences (WP-2.4, FR-4 G2). A snapshot
-- freezes the observed neighbor state per machine port; the G2 judge
-- compares it against the BOM expectation and classifies mismatches into
-- T1-T5. Snapshots are versioned history; differences reference the snapshot
-- and the expected port pair, and targeted re-verification simply takes a
-- new snapshot for the affected machines.
-- Rollback: DROP TABLE lldp_differences; DROP TABLE lldp_snapshots.
CREATE TABLE lldp_snapshots (
 id varchar(32) PRIMARY KEY,
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 revision int NOT NULL CHECK(revision >= 1),
 observed jsonb NOT NULL DEFAULT '{}',
 collected_by varchar(32) NOT NULL REFERENCES users(id),
 collected_at timestamptz NOT NULL,
 UNIQUE(machine_id, revision)
);
CREATE TABLE lldp_differences (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 snapshot_id varchar(32) NOT NULL REFERENCES lldp_snapshots(id),
 local_port varchar(64) NOT NULL,
 expected_peer varchar(120) NOT NULL DEFAULT '',
 expected_peer_port varchar(64) NOT NULL DEFAULT '',
 observed_peer varchar(120) NOT NULL DEFAULT '',
 observed_peer_port varchar(64) NOT NULL DEFAULT '',
 category varchar NOT NULL CHECK(category IN('T1','T2','T3','T4','T5')),
 detail jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL,
 resolved_at timestamptz,
 resolution_note text NOT NULL DEFAULT ''
);
CREATE INDEX lldp_differences_project ON lldp_differences(project_id, created_at DESC);
CREATE INDEX lldp_snapshots_machine ON lldp_snapshots(machine_id, revision DESC);
