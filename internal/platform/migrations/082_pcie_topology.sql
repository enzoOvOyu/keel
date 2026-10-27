-- WP-2.12: comparison consumes current G1's immutable original capture. It
-- cannot publish G2/G3 or close T7 using a browser-supplied verdict.
CREATE TABLE pci_topology_snapshots(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 version bigint NOT NULL CHECK(version>0),
 model_version_id varchar(32) NOT NULL REFERENCES device_model_versions(id),
 precollect_id varchar(32) NOT NULL REFERENCES validation_precollect_records(id),
 g1_id varchar(32) NOT NULL REFERENCES validation_gate_evidence(id),
 scope_hash varchar(64) NOT NULL,
 scope jsonb NOT NULL,
 origin varchar(4) NOT NULL CHECK(origin IN('real','sim')),
 raw_sha256 varchar(64) NOT NULL,
 snapshot jsonb NOT NULL,
 evaluation jsonb NOT NULL,
 verdict varchar(8) NOT NULL CHECK(verdict IN('PASS','FAIL','BLOCKED')),
 valid_until timestamptz NOT NULL,
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id,asset_id,version),
 UNIQUE(project_id,asset_id,scope_hash),
 CHECK(scope->>'scope_hash'=scope_hash AND scope->>'project_id'=project_id AND scope->>'asset_id'=asset_id AND scope->>'machine_id'=machine_id AND scope->>'model_version_id'=model_version_id AND scope->>'g1_id'=g1_id AND scope->>'precollect_id'=precollect_id AND scope->>'precollect_sha256'=raw_sha256),
 CHECK(snapshot->>'version'='keel.pcie-topology/v1' AND evaluation->>'rule_version'='pcie-topology/v1' AND evaluation->>'verdict'=verdict)
);
CREATE INDEX pci_topology_history ON pci_topology_snapshots(project_id,asset_id,created_at DESC,id DESC);
CREATE TABLE topology_issue_bindings(
 issue_id varchar(32) PRIMARY KEY REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 model_version_id varchar(32) NOT NULL REFERENCES device_model_versions(id),
 origin varchar(4) NOT NULL CHECK(origin IN('real','sim')),
 finding_key varchar(64) NOT NULL,
 UNIQUE(project_id,asset_id,model_version_id,origin,finding_key)
);
CREATE TABLE topology_diffs(
 id varchar(32) PRIMARY KEY,
 snapshot_id varchar(32) NOT NULL REFERENCES pci_topology_snapshots(id),
 ordinal integer NOT NULL CHECK(ordinal>=0),
 category varchar(32) NOT NULL CHECK(category IN('pcie_misplaced','numa_anomaly','nvlink_missing','lnk_degraded')),
 detail jsonb NOT NULL,
 issue_id varchar(32) REFERENCES topology_issue_bindings(issue_id),
 UNIQUE(snapshot_id,ordinal),
 CHECK(detail->>'category'=category AND detail->>'verdict' IN('FAIL','BLOCKED'))
);
CREATE TRIGGER pci_topology_immutable BEFORE UPDATE OR DELETE ON pci_topology_snapshots FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER topology_issue_binding_immutable BEFORE UPDATE OR DELETE ON topology_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER topology_diff_immutable BEFORE UPDATE OR DELETE ON topology_diffs FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_pci_topology_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='pci_topology_snapshots' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_gate_evidence g JOIN validation_precollect_requests q ON q.id=g.precollect_id JOIN validation_precollect_records r ON r.id=q.id JOIN project_assets a ON a.id=q.asset_id JOIN device_model_versions m ON m.id=NEW.model_version_id WHERE g.id=NEW.g1_id AND g.project_id=NEW.project_id AND g.asset_id=NEW.asset_id AND g.machine_id=NEW.machine_id AND g.precollect_id=NEW.precollect_id AND g.verdict='PASS' AND g.valid_until>statement_timestamp() AND NEW.valid_until<=g.valid_until AND NEW.valid_until>statement_timestamp() AND q.scope_hash=NEW.scope->>'precollect_scope_hash' AND r.raw_sha256=NEW.raw_sha256 AND r.origin=NEW.origin AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id AND a.model_version_id=m.id AND a.version=(q.scope->>'asset_version')::bigint AND m.content_hash=NEW.scope->>'model_content_hash' AND m.expected_topology=NEW.scope->'expectation' AND r.report->'topology'=NEW.snapshot AND (NEW.verdict<>'PASS' OR NEW.origin='real') AND NOT EXISTS(SELECT 1 FROM validation_gate_evidence newer WHERE newer.project_id=g.project_id AND newer.asset_id=g.asset_id AND (newer.created_at,newer.id)>(g.created_at,g.id))) THEN RAISE EXCEPTION 'PCIe topology requires current G1, independent model and original retained capture'; END IF;
 ELSIF TG_TABLE_NAME='topology_issue_bindings' THEN
  IF NOT EXISTS(SELECT 1 FROM issues i JOIN project_assets a ON a.id=NEW.asset_id WHERE i.id=NEW.issue_id AND i.project_id=NEW.project_id AND i.source='topology' AND i.category='T7' AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id) THEN RAISE EXCEPTION 'T7 binding must remain in the original asset scope'; END IF;
 ELSIF TG_TABLE_NAME='topology_diffs' THEN
  IF NEW.issue_id IS NOT NULL AND (NEW.detail->>'verdict'<>'FAIL' OR NOT EXISTS(SELECT 1 FROM pci_topology_snapshots s JOIN topology_issue_bindings b ON b.issue_id=NEW.issue_id WHERE s.id=NEW.snapshot_id AND b.project_id=s.project_id AND b.asset_id=s.asset_id AND b.machine_id=s.machine_id AND b.model_version_id=s.model_version_id AND b.origin=s.origin)) THEN RAISE EXCEPTION 'T7 observation must match the snapshot scope'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pci_topology_scope BEFORE INSERT ON pci_topology_snapshots FOR EACH ROW EXECUTE FUNCTION keel_pci_topology_scope();
CREATE TRIGGER topology_issue_binding_scope BEFORE INSERT ON topology_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_pci_topology_scope();
CREATE TRIGGER topology_diff_scope BEFORE INSERT ON topology_diffs FOR EACH ROW EXECUTE FUNCTION keel_pci_topology_scope();
