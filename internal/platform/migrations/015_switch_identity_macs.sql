-- Current switch MAC identities used by ZTP lookup. BOM revisions preserve
-- prior values; this table represents only the latest declared bindings.
CREATE TABLE switch_mac_bindings (
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 kind varchar(16) NOT NULL CHECK(kind IN('mgmt','base','other')),
 mac varchar(17) NOT NULL CHECK(mac ~ '^[0-9a-f]{2}(:[0-9a-f]{2}){5}$'),
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(switch_id,kind,mac),
 UNIQUE(mac)
);
CREATE UNIQUE INDEX switch_mac_bindings_primary_kind ON switch_mac_bindings(switch_id,kind) WHERE kind IN('mgmt','base');
