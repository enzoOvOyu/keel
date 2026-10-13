-- PCIe topology verification (WP-2.12, FR-4.8). Snapshots are versioned
-- per machine: the caller submits the collected raw state (per-device BDF,
-- upstream path chain, NUMA node, LnkCap/LnkSta, the nvidia-smi topo -m
-- raw text) plus the expected topology declared by the approved
-- DeviceModelVersion (never derived from the machine's own output). The
-- judge compares four dimensions — slot path ownership, NUMA layout,
-- NVLink connectivity, LnkSta-vs-LnkCap — and records TopologyDiff rows
-- categorized T7; unresolved diffs escalate after two failed targeted
-- repairs (escalation flag, never auto-RMA).
-- Rollback: DROP TABLE pcie_diffs; DROP TABLE pcie_snapshots.
CREATE TABLE pcie_snapshots (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 revision int NOT NULL CHECK(revision >= 1),
 devices jsonb NOT NULL DEFAULT '[]',
 expected jsonb NOT NULL DEFAULT '{}',
 nvlink_raw text NOT NULL DEFAULT '',
 collected_by varchar(32) NOT NULL REFERENCES users(id),
 collected_at timestamptz NOT NULL,
 UNIQUE(machine_id, revision)
);
CREATE TABLE pcie_diffs (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 snapshot_id varchar(32) NOT NULL REFERENCES pcie_snapshots(id),
 dimension varchar NOT NULL CHECK(dimension IN('slot_path','numa','nvlink','link_width')),
 detail jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL,
 resolved_at timestamptz,
 repair_count int NOT NULL DEFAULT 0,
 escalated bool NOT NULL DEFAULT false
);
CREATE INDEX pcie_diffs_machine ON pcie_diffs(machine_id, created_at DESC);
