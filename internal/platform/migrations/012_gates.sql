-- Gate evidence registry (WP-2.3, FR-4 G1 / gate contract). Evidence is
-- immutable history: re-evaluation inserts a new row, never overwrites.
-- Conclusions are PASS/FAIL/BLOCKED/WAIVED and skipping never records PASS.
-- Waivers require approver, reason, scope and expiry. Downstream gates
-- check the newest evidence whose valid_until still holds.
-- Rollback: DROP TABLE gate_evidences.
CREATE TABLE gate_evidences (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 gate varchar NOT NULL CHECK(gate IN('G1','G2','G3','G4','G5','G6')),
 target_hash varchar(64) NOT NULL,
 targets jsonb NOT NULL DEFAULT '[]',
 conclusion varchar NOT NULL CHECK(conclusion IN('PASS','FAIL','BLOCKED','WAIVED')),
 detail jsonb NOT NULL DEFAULT '{}',
 decided_by varchar(32) NOT NULL REFERENCES users(id),
 valid_until timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 waiver_approver varchar(32) REFERENCES users(id),
 waiver_reason text NOT NULL DEFAULT '',
 waiver_scope text NOT NULL DEFAULT '',
 waiver_expires timestamptz
);
CREATE INDEX gate_evidences_project ON gate_evidences(project_id, gate, created_at DESC);
