-- WP-1.15: one versioned, cross-domain BOM package and stable material IDs.
-- Mapping profiles and switch model kind are the minimal prerequisites agreed
-- for import validation; later work packages add the full management UI.
ALTER TABLE device_models DROP CONSTRAINT device_models_kind_check;
ALTER TABLE device_models ADD CONSTRAINT device_models_kind_check CHECK(kind IN('rack','server','compute-tray','switch'));

CREATE TABLE bom_mapping_profiles (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 version_ref varchar(120) NOT NULL,
 state varchar NOT NULL CHECK(state IN('draft','approved','retired')),
 server_columns jsonb NOT NULL DEFAULT '{}',
 fabric_columns jsonb NOT NULL DEFAULT '{}',
 created_by varchar(32) NOT NULL REFERENCES users(id),
 approved_by varchar(32) REFERENCES users(id),
 created_at timestamptz NOT NULL,
 approved_at timestamptz,
 UNIQUE(project_id,version_ref)
);

CREATE TABLE bom_import_packages (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 bom_import_batch_id varchar(160) NOT NULL,
 schema_version varchar(40) NOT NULL CHECK(schema_version='bom-import/v1'),
 profile_version_ref varchar(120) NOT NULL,
 content_hash varchar(71) NOT NULL CHECK(content_hash ~ '^sha256:[0-9a-f]{64}$'),
 declared_by varchar(40) NOT NULL,
 manifest jsonb NOT NULL,
 row_count bigint NOT NULL CHECK(row_count>=0),
 state varchar NOT NULL CHECK(state IN('parsing','precheck','ready','blocked','committed')),
 uploaded_by varchar(32) NOT NULL REFERENCES users(id),
 committed_at timestamptz NOT NULL,
 UNIQUE(project_id,bom_import_batch_id),
 FOREIGN KEY(project_id,profile_version_ref) REFERENCES bom_mapping_profiles(project_id,version_ref)
);
CREATE INDEX bom_import_packages_project ON bom_import_packages(project_id,committed_at DESC);

CREATE TABLE bom_import_manifest_files (
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id) ON DELETE RESTRICT,
 file_path varchar(300) NOT NULL,
 domain varchar(40) NOT NULL,
 entity varchar(40) NOT NULL,
 columns jsonb NOT NULL,
 row_count bigint NOT NULL CHECK(row_count>=0),
 file_hash varchar(64) NOT NULL CHECK(file_hash ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(package_id,file_path)
);
CREATE TABLE bom_import_issues (
 id varchar(32) PRIMARY KEY,
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id) ON DELETE RESTRICT,
 severity varchar NOT NULL CHECK(severity IN('BLOCK','WARN','INFO')),
 file_path varchar(300) NOT NULL,
 bom_line_no bigint,
 field varchar(120),
 code varchar(120) NOT NULL,
 created_at timestamptz NOT NULL
);

-- bom_items is the current identity registry. A new batch updates the same
-- item_uid for the same physical item, while bom_item_revisions preserves the
-- exact batch fact. A replacement with a new serial receives a new identity.
CREATE TABLE bom_items (
 item_uid varchar(32) PRIMARY KEY,
 category varchar NOT NULL CHECK(category IN('server','switch','nic','gpu','psu','bmc','port','optic_module','cable','other')),
 sub_type varchar(120) NOT NULL DEFAULT '',
 model_version_id varchar(32) REFERENCES device_model_versions(id),
 vendor varchar(120) NOT NULL DEFAULT '',
 serial varchar(120),
 position jsonb NOT NULL DEFAULT '{}',
 parent_uid varchar(32) REFERENCES bom_items(item_uid),
 endpoint_a_uid varchar(32) REFERENCES bom_items(item_uid),
 endpoint_b_uid varchar(32) REFERENCES bom_items(item_uid),
 port_index varchar(64),
 port_name varchar(64),
 port_role varchar(20),
 rail varchar(40),
 pod varchar(40),
 role varchar(40),
 mac_list jsonb NOT NULL DEFAULT '[]',
 credential_batch_ref varchar(120),
 expect_version jsonb NOT NULL DEFAULT '{}',
 source_of_record varchar NOT NULL CHECK(source_of_record IN('design_bom','discovered','manual')),
 status varchar NOT NULL DEFAULT 'planned' CHECK(status IN('planned','imported','verified','installed','retired')),
 first_project_id varchar(32) NOT NULL REFERENCES projects(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(category<>'cable' OR (endpoint_a_uid IS NOT NULL AND endpoint_b_uid IS NOT NULL AND endpoint_a_uid<endpoint_b_uid)),
 CHECK(category<>'port' OR (parent_uid IS NOT NULL AND port_index IS NOT NULL))
);
CREATE UNIQUE INDEX bom_items_serial_identity ON bom_items(category,serial) WHERE serial IS NOT NULL AND serial<>'';
CREATE UNIQUE INDEX bom_items_port_identity ON bom_items(parent_uid,port_index) WHERE category='port';
CREATE UNIQUE INDEX bom_items_cable_identity ON bom_items(endpoint_a_uid,endpoint_b_uid) WHERE category='cable';
CREATE INDEX bom_items_parent ON bom_items(parent_uid);

CREATE TABLE bom_item_revisions (
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id) ON DELETE RESTRICT,
 item_uid varchar(32) NOT NULL REFERENCES bom_items(item_uid) ON DELETE RESTRICT,
 bom_line_no bigint NOT NULL CHECK(bom_line_no>0),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 row_snapshot jsonb NOT NULL,
 recorded_at timestamptz NOT NULL,
 PRIMARY KEY(package_id,bom_line_no),
 UNIQUE(package_id,item_uid)
);
CREATE INDEX bom_item_revisions_item ON bom_item_revisions(item_uid,recorded_at DESC);

-- Operational projections keep the P1 machine and port APIs usable. Network
-- port rows have a switch owner; server port rows keep their machine owner.
CREATE TABLE switches (
 id varchar(32) PRIMARY KEY REFERENCES bom_items(item_uid),
 serial_number varchar(120) NOT NULL UNIQUE,
 hostname varchar(253) NOT NULL,
 model_version_id varchar(32) REFERENCES device_model_versions(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE components (
 id varchar(32) PRIMARY KEY REFERENCES bom_items(item_uid),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 category varchar NOT NULL CHECK(category IN('nic','gpu','psu','bmc')),
 serial_number varchar(120),
 slot varchar(64),
 created_at timestamptz NOT NULL
);
ALTER TABLE physical_ports ADD COLUMN switch_id varchar(32) REFERENCES switches(id);
ALTER TABLE physical_ports ADD COLUMN parent_uid varchar(32) REFERENCES bom_items(item_uid);
ALTER TABLE physical_ports ADD COLUMN port_index varchar(64);
ALTER TABLE physical_ports ALTER COLUMN machine_id DROP NOT NULL;
ALTER TABLE physical_ports ALTER COLUMN mac_address DROP NOT NULL;
ALTER TABLE physical_ports ADD CONSTRAINT physical_ports_one_owner CHECK((machine_id IS NOT NULL) <> (switch_id IS NOT NULL));
CREATE UNIQUE INDEX physical_ports_parent_index ON physical_ports(parent_uid,port_index) WHERE parent_uid IS NOT NULL;
