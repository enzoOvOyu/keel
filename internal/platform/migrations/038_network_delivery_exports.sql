-- FR-0.6.8/0.8.6: references and immutable archived JSON, not a second CMDB/BOM.
CREATE TABLE network_delivery_exports (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id),
 revision integer NOT NULL CHECK(revision>0),
 kind varchar(16) NOT NULL CHECK(kind IN('draft','delivery')),
 schema_version varchar(40) NOT NULL CHECK(schema_version='network-delivery/v1'),
 scope_hash varchar(64) NOT NULL,
 preflight_hash varchar(64) NOT NULL,
 content_hash varchar(64) NOT NULL,
 consistency_check_rate double precision NOT NULL CHECK(consistency_check_rate BETWEEN 0 AND 1),
 manifest jsonb NOT NULL,
 content bytea NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id,revision),
 CHECK(kind<>'delivery' OR consistency_check_rate=1)
);
CREATE INDEX network_delivery_exports_recent ON network_delivery_exports(project_id,created_at DESC,id DESC);
CREATE TRIGGER network_delivery_exports_immutable BEFORE UPDATE OR DELETE ON network_delivery_exports FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
