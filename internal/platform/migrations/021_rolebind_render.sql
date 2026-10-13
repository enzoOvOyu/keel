-- Network role bindings and the batch-configuration renderers (WP-3.2,
-- FR-5.2–5.5, FR-5.9). Configuration templates reference logical roles only
-- — management/storage/compute/oob — never a NIC model or an interface
-- name. A role binds to a model slot (the model declares a default role per
-- slot) and the project may override that binding per slot, so a staged
-- build (phase two without storage) or a NIC substitution is absorbed by an
-- override instead of a template edit. The resolved binding is written to
-- PortModel (physical_ports.role); the panel, connectivity tests and issues
-- all read it from there.
-- Rollback: DROP TABLE roce_imports; DROP TABLE nic_partitions;
-- DROP TABLE project_role_plans; ALTER TABLE physical_ports DROP COLUMN role;
ALTER TABLE physical_ports ADD COLUMN role varchar NOT NULL DEFAULT 'unused';
ALTER TABLE physical_ports ADD CONSTRAINT physical_ports_role_check
  CHECK(role IN('management','storage','compute','oob','unused'));
CREATE INDEX physical_ports_role ON physical_ports(machine_id, role);

CREATE TABLE project_role_plans (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL UNIQUE REFERENCES projects(id),
 roles jsonb NOT NULL DEFAULT '{}',
 slot_overrides jsonb NOT NULL DEFAULT '{}',
 updated_by varchar(32) NOT NULL REFERENCES users(id),
 updated_at timestamptz NOT NULL
);

-- FR-5.3: partitioning registers per-port rate, total bandwidth and PF/VF
-- capability separately; the before/after pair is what the diff preview
-- renders (the after state is only written on an explicit apply).
CREATE TABLE nic_partitions (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 role varchar NOT NULL DEFAULT 'unused',
 pf_count int NOT NULL DEFAULT 1 CHECK(pf_count >= 0),
 vf_count int NOT NULL DEFAULT 0 CHECK(vf_count >= 0),
 rate_gbps int NOT NULL DEFAULT 0 CHECK(rate_gbps >= 0),
 before_state jsonb NOT NULL DEFAULT '{}',
 after_state jsonb NOT NULL DEFAULT '{}',
 updated_by varchar(32) NOT NULL REFERENCES users(id),
 updated_at timestamptz NOT NULL,
 UNIQUE(project_id, port_id)
);

-- FR-5.5: a RoCE import records the pre-state, the applied state and the
-- read-back result; the receipt is the evidence that the target actually
-- took the configuration (verified is never set without a matching
-- read-back).
CREATE TABLE roce_imports (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 config_set_id varchar(32) NOT NULL REFERENCES config_sets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 before jsonb NOT NULL DEFAULT '{}',
 after jsonb NOT NULL DEFAULT '{}',
 readback jsonb NOT NULL DEFAULT '{}',
 verified bool NOT NULL DEFAULT false,
 receipt text NOT NULL DEFAULT '',
 imported_by varchar(32) NOT NULL REFERENCES users(id),
 imported_at timestamptz NOT NULL
);
CREATE INDEX roce_imports_machine ON roce_imports(machine_id, imported_at DESC);
