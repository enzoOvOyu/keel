CREATE TABLE fabric_diagnostic_records (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 collection_id varchar(32) NOT NULL REFERENCES fabric_collection_runs(id),
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id),
 device_id varchar(32) NOT NULL REFERENCES switches(id),
 port_scope varchar(128) NOT NULL,
 evidence_origin varchar(20) NOT NULL CHECK(evidence_origin IN('device','protocol_fixture')),
 content_hash char(64) NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 report jsonb NOT NULL,
 valid_until timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL,
 UNIQUE(project_id,device_id,port_scope,evidence_origin,content_hash)
);
CREATE TABLE fabric_port_issue_bindings (
 issue_id varchar(32) PRIMARY KEY REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 port_uid varchar(32) NOT NULL REFERENCES physical_ports(id),
 category varchar(2) NOT NULL CHECK(category IN('T2','T3','T4','T6')),
 subtype varchar(32) NOT NULL,
 rule_id varchar(128) NOT NULL,
 evidence_origin varchar(20) NOT NULL CHECK(evidence_origin IN('device','protocol_fixture')),
 UNIQUE(project_id,port_uid,category,subtype,rule_id,evidence_origin)
);
CREATE TABLE fabric_port_issue_observations (
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL REFERENCES fabric_port_issue_bindings(issue_id),
 record_id varchar(32) NOT NULL REFERENCES fabric_diagnostic_records(id),
 port_name varchar(128) NOT NULL,
 check_result jsonb NOT NULL CHECK(check_result->>'state' IS NOT DISTINCT FROM 'anomaly'),
 UNIQUE(issue_id,record_id)
);
CREATE INDEX fabric_port_issue_history ON fabric_port_issue_observations(issue_id,record_id);
CREATE FUNCTION keel_fabric_port_evidence_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='fabric_diagnostic_records' THEN
  IF NOT EXISTS(SELECT 1 FROM fabric_collection_runs r JOIN fabric_switch_samples s ON s.run_id=r.id AND s.switch_id=NEW.device_id
   WHERE r.id=NEW.collection_id AND r.project_id=NEW.project_id AND r.package_id=NEW.package_id
   AND r.report->>'evidence_source'=NEW.evidence_origin AND NEW.valid_until<=r.valid_until)
   OR NEW.report->>'project_id' IS DISTINCT FROM NEW.project_id OR NEW.report->>'package_id' IS DISTINCT FROM NEW.package_id
   OR NEW.report->>'device_id' IS DISTINCT FROM NEW.device_id OR NEW.report->>'np3' IS DISTINCT FROM 'BLOCKED'
   OR NEW.report->>'record_id' IS DISTINCT FROM NEW.id OR NEW.report->>'collection_id' IS DISTINCT FROM NEW.collection_id THEN
   RAISE EXCEPTION 'Diagnostic record must match a scoped immutable collection';
  END IF;
 ELSIF TG_TABLE_NAME='fabric_port_issue_bindings' THEN
  IF NOT EXISTS(SELECT 1 FROM issues i WHERE i.id=NEW.issue_id AND i.project_id=NEW.project_id
    AND i.source='fabric' AND i.category=NEW.category AND i.phase='delivery'
    AND i.anchors @> jsonb_build_array(jsonb_build_object('kind','port','id',NEW.port_uid))) THEN
   RAISE EXCEPTION 'Port issue identity does not match its discovery';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM fabric_diagnostic_records r JOIN fabric_port_issue_bindings b ON b.issue_id=NEW.issue_id
   JOIN physical_ports p ON p.id=b.port_uid AND p.switch_id=r.device_id
   CROSS JOIN LATERAL jsonb_array_elements(r.report->'ports') port
   CROSS JOIN LATERAL jsonb_array_elements(port->'checks') chk
   WHERE r.id=NEW.record_id AND r.project_id=b.project_id AND r.evidence_origin=b.evidence_origin
   AND port->>'port'=NEW.port_name AND chk=NEW.check_result
   AND chk->>'issue_type'=b.category AND coalesce(chk->>'subtype','')=b.subtype
   AND split_part(chk->>'rule_ref','@',1)=b.rule_id) THEN
   RAISE EXCEPTION 'Port issue observation does not match diagnostic evidence';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER check_fabric_diagnostic_record BEFORE INSERT ON fabric_diagnostic_records FOR EACH ROW EXECUTE FUNCTION keel_fabric_port_evidence_scope();
CREATE TRIGGER check_fabric_port_issue_binding BEFORE INSERT ON fabric_port_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_fabric_port_evidence_scope();
CREATE TRIGGER check_fabric_port_issue_observation BEFORE INSERT ON fabric_port_issue_observations FOR EACH ROW EXECUTE FUNCTION keel_fabric_port_evidence_scope();
CREATE TRIGGER immutable_fabric_diagnostic_record BEFORE UPDATE OR DELETE ON fabric_diagnostic_records FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER immutable_fabric_port_issue_binding BEFORE UPDATE OR DELETE ON fabric_port_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER immutable_fabric_port_issue_observation BEFORE UPDATE OR DELETE ON fabric_port_issue_observations FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
