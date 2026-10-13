-- Mapping revisions (WP-2.5, FR-4 G3, FR 3.4.4). A revision is immutable
-- once published: it binds every physical port of a machine to its OS
-- interface name (planned naming), permanent MAC, BDF, switch port and
-- GPU affinity. Publishing requires valid G1 evidence for the target set
-- and unresolved-line-order absence (G2 semantics); history is kept on
-- part replacement — a new revision references the previous one instead of
-- overwriting entries.
-- Rollback: DROP TABLE mapping_entries; DROP TABLE mapping_revisions.
CREATE TABLE mapping_revisions (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 sequence int NOT NULL CHECK(sequence >= 1),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 gate_evidence_id varchar(32) REFERENCES gate_evidences(id),
 note text NOT NULL DEFAULT '',
 published_by varchar(32) NOT NULL REFERENCES users(id),
 published_at timestamptz NOT NULL,
 previous_revision varchar(32) REFERENCES mapping_revisions(id),
 UNIQUE(project_id, machine_id, sequence)
);
CREATE TABLE mapping_entries (
 id varchar(32) PRIMARY KEY,
 revision_id varchar(32) NOT NULL REFERENCES mapping_revisions(id),
 port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 os_interface varchar(64) NOT NULL,
 mac_address varchar(17) NOT NULL,
 bdf varchar(12) NOT NULL DEFAULT '',
 switch_port varchar(64) NOT NULL DEFAULT '',
 gpu_affinity varchar(32) NOT NULL DEFAULT '',
 UNIQUE(revision_id, port_id)
);
CREATE INDEX mapping_entries_revision ON mapping_entries(revision_id);
