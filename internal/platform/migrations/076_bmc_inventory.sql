-- Durable domain boundary for the shared-executor collector. No scheduler,
-- credential or real hardware endpoint is activated by this migration.
CREATE TABLE bmc_collection_requests (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 asset_version bigint NOT NULL CHECK(asset_version>0),
 level varchar(2) NOT NULL CHECK(level IN('L1','L2','L3')),
 origin varchar(4) NOT NULL CHECK(origin IN('sim','real')),
 credential_batch_ref varchar(120) NOT NULL,
 credential_version varchar(120) NOT NULL,
 target jsonb NOT NULL,
 execution_id varchar(32) NOT NULL UNIQUE REFERENCES exec_executions(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL
);
CREATE TABLE bmc_collection_records (
 id varchar(32) PRIMARY KEY REFERENCES bmc_collection_requests(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 level varchar(2) NOT NULL CHECK(level IN('L1','L2','L3')),
 origin varchar(4) NOT NULL CHECK(origin IN('sim','real')),
 state varchar(12) NOT NULL CHECK(state IN('completed','partial','failed')),
 parser_version varchar(80) NOT NULL,
 collected_at timestamptz NOT NULL,
 received_at timestamptz NOT NULL,
 content_hash varchar(64) NOT NULL,
 report jsonb NOT NULL
);
CREATE INDEX bmc_collection_records_recent ON bmc_collection_records(project_id,asset_id,received_at DESC,id DESC);
CREATE TABLE bmc_collection_resources (
 collection_id varchar(32) NOT NULL REFERENCES bmc_collection_records(id),
 resource_path varchar(2048) NOT NULL,
 resource_type text NOT NULL,
 structured jsonb NOT NULL,
 data_sha256 varchar(64) NOT NULL,
 raw_key text NOT NULL,
 raw_version text NOT NULL CHECK(length(raw_version)>0 AND raw_version<>'null'),
 raw_sha256 varchar(64) NOT NULL,
 raw_size bigint NOT NULL CHECK(raw_size BETWEEN 1 AND 2097152),
 received_at timestamptz NOT NULL,
 PRIMARY KEY(collection_id,resource_path)
);
CREATE TRIGGER bmc_request_immutable BEFORE UPDATE OR DELETE ON bmc_collection_requests FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER bmc_record_immutable BEFORE UPDATE OR DELETE ON bmc_collection_records FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER bmc_resource_immutable BEFORE UPDATE OR DELETE ON bmc_collection_resources FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_bmc_inventory_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE request bmc_collection_requests%ROWTYPE;
BEGIN
 IF TG_TABLE_NAME='bmc_collection_requests' THEN
  IF NOT EXISTS(SELECT 1 FROM project_assets a JOIN machines m ON m.id=a.machine_id JOIN exec_executions x ON x.id=NEW.execution_id WHERE a.id=NEW.asset_id AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id AND a.version=NEW.asset_version AND x.project_id=NEW.project_id) OR NEW.target->>'request_id' IS DISTINCT FROM NEW.id OR NEW.target->>'project_id' IS DISTINCT FROM NEW.project_id OR NEW.target->>'asset_id' IS DISTINCT FROM NEW.asset_id OR NEW.target->>'machine_id' IS DISTINCT FROM NEW.machine_id OR NEW.target->>'credential_batch_ref' IS DISTINCT FROM NEW.credential_batch_ref OR NEW.target->>'credential_version' IS DISTINCT FROM NEW.credential_version OR (NEW.target->>'asset_version')::bigint IS DISTINCT FROM NEW.asset_version THEN RAISE EXCEPTION 'BMC request must bind one current project asset, frozen credentials and execution'; END IF;
 ELSIF TG_TABLE_NAME='bmc_collection_records' THEN
  SELECT * INTO STRICT request FROM bmc_collection_requests WHERE id=NEW.id;
  IF NEW.project_id<>request.project_id OR NEW.asset_id<>request.asset_id OR NEW.machine_id<>request.machine_id OR NEW.execution_id<>request.execution_id OR NEW.level<>request.level OR NEW.origin<>request.origin OR NEW.report->'target' IS DISTINCT FROM request.target OR NEW.report->>'level' IS DISTINCT FROM request.level OR NEW.report->>'origin' IS DISTINCT FROM request.origin OR NEW.report->>'state' IS DISTINCT FROM NEW.state OR NEW.report->>'parser_version' IS DISTINCT FROM NEW.parser_version THEN RAISE EXCEPTION 'BMC result must match its immutable scoped request'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bmc_request_scope BEFORE INSERT ON bmc_collection_requests FOR EACH ROW EXECUTE FUNCTION keel_bmc_inventory_scope();
CREATE TRIGGER bmc_record_scope BEFORE INSERT ON bmc_collection_records FOR EACH ROW EXECUTE FUNCTION keel_bmc_inventory_scope();
