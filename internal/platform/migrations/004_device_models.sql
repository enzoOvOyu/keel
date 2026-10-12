-- Device model library (WP-1.12, FR-14). Stable model identity with immutable
-- published versions; slots are expectation data, not runtime state.
-- Rollback: DROP TABLE device_model_slots, device_model_versions, device_models.
CREATE TABLE device_models (
 id varchar(32) PRIMARY KEY,
 scope_key varchar NOT NULL CHECK(scope_key = 'global' OR scope_key LIKE 'project:%'),
 name varchar(120) NOT NULL,
 kind varchar NOT NULL CHECK(kind IN('rack','server','compute-tray')),
 state varchar NOT NULL DEFAULT 'active' CHECK(state IN('active','retired')),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(scope_key, name)
);
CREATE INDEX device_models_scope ON device_models(scope_key, name);
CREATE TABLE device_model_versions (
 id varchar(32) PRIMARY KEY,
 model_id varchar(32) NOT NULL REFERENCES device_models(id),
 version bigint NOT NULL CHECK(version >= 1),
 hw_spec jsonb NOT NULL DEFAULT '{}',
 expected_topology jsonb NOT NULL DEFAULT '{}',
 capability_matrix jsonb NOT NULL DEFAULT '{}',
 panel_layout jsonb NOT NULL DEFAULT '{}',
 bom_expect jsonb NOT NULL DEFAULT '{}',
 default_role_plan jsonb NOT NULL DEFAULT '{}',
 content_hash varchar(64) NOT NULL,
 published_at timestamptz NOT NULL,
 UNIQUE(model_id, version)
);
CREATE TABLE device_model_slots (
 id varchar(32) PRIMARY KEY,
 model_version_id varchar(32) NOT NULL REFERENCES device_model_versions(id),
 slot_no integer NOT NULL CHECK(slot_no >= 1),
 pcie_slot varchar(64) NOT NULL,
 expect_nic_model varchar(120) NOT NULL,
 panel_port varchar(64) NOT NULL,
 default_role varchar NOT NULL CHECK(default_role IN('management','storage','compute','oob','unused')),
 UNIQUE(model_version_id, slot_no)
);
