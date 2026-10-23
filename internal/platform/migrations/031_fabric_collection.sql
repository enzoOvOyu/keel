-- N-S5 network-only observations never create server PortModel or NP-3 PASS.
CREATE TABLE fabric_collection_runs (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id),
 report jsonb NOT NULL,
 source_hash varchar(64) NOT NULL,
 collected_at timestamptz NOT NULL,
 valid_until timestamptz NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id)
);
CREATE INDEX fabric_collection_runs_recent ON fabric_collection_runs(project_id,collected_at DESC,id DESC);
CREATE TABLE fabric_switch_samples (
 run_id varchar(32) NOT NULL REFERENCES fabric_collection_runs(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 content_hash varchar(64) NOT NULL,
 sample jsonb NOT NULL,
 PRIMARY KEY(run_id,switch_id)
);
CREATE FUNCTION keel_fabric_evidence_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'Fabric evidence is immutable'; END $$;
CREATE TRIGGER fabric_collection_immutable BEFORE UPDATE OR DELETE ON fabric_collection_runs FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER fabric_switch_samples_immutable BEFORE UPDATE OR DELETE ON fabric_switch_samples FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
