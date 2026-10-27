CREATE TABLE validation_wiring_issue_bindings(
 issue_id varchar(32) PRIMARY KEY REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 physical_port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 category varchar(2) NOT NULL CHECK(category IN('T1','T2','T6')),
 host_origin varchar(8) NOT NULL CHECK(host_origin IN('real','sim')),
 switch_origin varchar(20) NOT NULL CHECK(switch_origin IN('device','protocol_fixture')),
 UNIQUE(project_id,asset_id,physical_port_id,category,host_origin,switch_origin)
);
CREATE TABLE validation_wiring_issue_observations(
 issue_id varchar(32) NOT NULL REFERENCES validation_wiring_issue_bindings(issue_id),
 report_id varchar(32) NOT NULL REFERENCES validation_wiring_step_reports(id),
 command_id varchar(32) NOT NULL REFERENCES validation_wiring_capture_records(command_id),
 finding_hash char(64) NOT NULL CHECK(finding_hash ~ '^[0-9a-f]{64}$'),
 finding jsonb NOT NULL,
 observed_at timestamptz NOT NULL,
 PRIMARY KEY(issue_id,command_id,finding_hash)
);
CREATE TRIGGER validation_wiring_issue_binding_immutable BEFORE UPDATE OR DELETE ON validation_wiring_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_issue_observation_immutable BEFORE UPDATE OR DELETE ON validation_wiring_issue_observations FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_wiring_issue_observation_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id JOIN validation_wiring_commands c ON c.command_id=NEW.command_id JOIN validation_wiring_capture_records evidence ON evidence.command_id=c.command_id JOIN validation_wiring_step_reports r ON r.id=NEW.report_id WHERE b.issue_id=NEW.issue_id AND i.source='g2' AND i.project_id=b.project_id AND i.category=b.category AND c.project_id=b.project_id AND c.asset_id=b.asset_id AND c.command->>'host_origin'=b.host_origin AND c.origin=b.switch_origin AND r.detail->>'command_id'=c.command_id AND r.batch_id=c.batch_id AND r.asset_id=c.asset_id AND r.step=(NEW.finding->>'step')::int AND r.source_hash=evidence.raw_sha256 AND NEW.finding->>'verdict'='FAIL' AND NEW.finding->>'physical_port_id'=b.physical_port_id AND NEW.finding->>'category'=b.category AND NEW.observed_at=evidence.stopped_at AND EXISTS(SELECT 1 FROM jsonb_array_elements(evidence.evaluation->'findings') f WHERE f=NEW.finding)) THEN RAISE EXCEPTION 'wiring issue must retain the original concrete physical-port failure'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_issue_observation_scope BEFORE INSERT ON validation_wiring_issue_observations FOR EACH ROW EXECUTE FUNCTION keel_wiring_issue_observation_scope();
