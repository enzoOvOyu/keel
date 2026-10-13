-- Machine identity registry (WP-1.7, FR-2.1). Machine/Component/PhysicalPort
-- identities are global and stable; ProjectAsset binds a machine into one
-- project with a model reference (FR-14.2). BOM imports are two-phase
-- (preview then commit) and idempotent per file version.
-- Rollback: DROP TABLE bom_imports, project_assets, physical_ports, machines.
CREATE TABLE machines (
 id varchar(32) PRIMARY KEY,
 serial_number varchar(120) NOT NULL UNIQUE,
 rack varchar(120) NOT NULL DEFAULT '',
 unit_position varchar(16) NOT NULL DEFAULT '',
 bmc_address varchar(64) NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE physical_ports (
 id varchar(32) PRIMARY KEY,
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 mac_address varchar(17) NOT NULL UNIQUE CHECK(mac_address ~ '^[0-9a-f]{2}(:[0-9a-f]{2}){5}$'),
 port_name varchar(64) NOT NULL,
 rail smallint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL,
 UNIQUE(machine_id, port_name)
);
CREATE TABLE project_assets (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 model_id varchar(32) REFERENCES device_models(id),
 hostname varchar(253) NOT NULL,
 ip_reference varchar(64) NOT NULL DEFAULT '',
 state varchar NOT NULL DEFAULT 'planned' CHECK(state IN('planned','provisioning','provisioned','failed')),
 version bigint NOT NULL DEFAULT 1 CHECK(version >= 1),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(project_id, machine_id),
 UNIQUE(project_id, hostname)
);
CREATE TABLE bom_imports (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 file_version varchar(120) NOT NULL,
 imported_by varchar(32) NOT NULL REFERENCES users(id),
 machines_created int NOT NULL DEFAULT 0,
 ports_created int NOT NULL DEFAULT 0,
 assets_created int NOT NULL DEFAULT 0,
 assets_updated int NOT NULL DEFAULT 0,
 committed_at timestamptz NOT NULL,
 UNIQUE(project_id, file_version)
);
CREATE INDEX project_assets_project ON project_assets(project_id, hostname);
CREATE INDEX physical_ports_machine ON physical_ports(machine_id);
