-- G1 evidence uses ordinary SSH Executions, existing stable BOM identities and
-- versioned raw storage. It creates no LLDP/topology/mapping publication.
ALTER TABLE project_assets DROP CONSTRAINT project_assets_state_check;
ALTER TABLE project_assets ADD CONSTRAINT project_assets_state_check CHECK(state IN('planned','provisioning','provisioned','trust_established','collected','lldp','mapped','configured','netverified','tested','maintained','failed'));
CREATE TABLE validation_precollect_requests(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 scope_hash varchar(64) NOT NULL,
 scope jsonb NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL,
 UNIQUE(execution_id,machine_id),
 CHECK(scope->>'project_id'=project_id AND scope->>'asset_id'=asset_id AND scope->>'machine_id'=machine_id AND scope->>'scope_hash'=scope_hash)
);
CREATE INDEX validation_precollect_history ON validation_precollect_requests(project_id,asset_id,created_at DESC,id DESC);
CREATE TABLE validation_precollect_records(
 id varchar(32) PRIMARY KEY REFERENCES validation_precollect_requests(id),
 attempt_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(id),
 command_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(command_id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 origin varchar(4) NOT NULL CHECK(origin IN('sim','real')),
 collected_at timestamptz NOT NULL,
 raw_key text NOT NULL,
 raw_version text NOT NULL CHECK(length(raw_version)>0 AND raw_version<>'null'),
 raw_sha256 varchar(64) NOT NULL,
 raw_size bigint NOT NULL CHECK(raw_size BETWEEN 1 AND 2097152),
 report jsonb NOT NULL,
 received_at timestamptz NOT NULL
);
CREATE TABLE validation_precollect_ports(
 record_id varchar(32) NOT NULL REFERENCES validation_precollect_records(id),
 physical_port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 component_id varchar(32) NOT NULL REFERENCES components(id),
 observation jsonb NOT NULL,
 PRIMARY KEY(record_id,physical_port_id)
);
CREATE TABLE validation_gate_evidence(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 gate varchar(2) NOT NULL CHECK(gate='G1'),
 precollect_id varchar(32) NOT NULL UNIQUE REFERENCES validation_precollect_records(id),
 scope_hash varchar(64) NOT NULL,
 revision_fingerprint varchar(64) NOT NULL,
 rule_version varchar(120) NOT NULL,
 verdict varchar(8) NOT NULL CHECK(verdict IN('PASS','FAIL','BLOCKED','WAIVED')),
 evaluation jsonb NOT NULL,
 observed_at timestamptz NOT NULL,
 valid_until timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 CHECK(valid_until>observed_at)
);
CREATE INDEX validation_gate_evidence_current ON validation_gate_evidence(project_id,asset_id,gate,created_at DESC,id DESC);
CREATE TRIGGER validation_precollect_request_immutable BEFORE UPDATE OR DELETE ON validation_precollect_requests FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER validation_precollect_record_immutable BEFORE UPDATE OR DELETE ON validation_precollect_records FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER validation_precollect_port_immutable BEFORE UPDATE OR DELETE ON validation_precollect_ports FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER validation_gate_evidence_immutable BEFORE UPDATE OR DELETE ON validation_gate_evidence FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_validation_precollect_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='validation_precollect_requests' THEN
  IF NOT EXISTS(SELECT 1 FROM project_assets a JOIN exec_executions x ON x.id=NEW.execution_id WHERE a.id=NEW.asset_id AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id AND a.version=(NEW.scope->>'asset_version')::bigint AND a.model_version_id=NEW.scope->>'model_version_id' AND x.project_id=NEW.project_id AND x.mode='ssh' AND x.parameters->>'validation_kind'='precollect') THEN RAISE EXCEPTION 'precollection must bind an existing project asset and domain-owned SSH execution'; END IF;
 ELSIF TG_TABLE_NAME='validation_precollect_records' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_precollect_requests q JOIN exec_task_attempts a ON a.execution_id=q.execution_id JOIN exec_targets t ON t.id=a.target_id AND t.resource_key=q.machine_id WHERE q.id=NEW.id AND a.id=NEW.attempt_id AND a.command_id=NEW.command_id AND a.fencing_token=NEW.fencing_token AND a.runner='ssh') OR NEW.report->>'origin' IS DISTINCT FROM NEW.origin OR (NEW.report->>'collected_at')::timestamptz IS DISTINCT FROM NEW.collected_at THEN RAISE EXCEPTION 'precollection evidence must match original frozen attempt'; END IF;
 ELSIF TG_TABLE_NAME='validation_precollect_ports' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_precollect_records r JOIN validation_precollect_requests q ON q.id=r.id JOIN physical_ports p ON p.id=NEW.physical_port_id JOIN components c ON c.id=NEW.component_id WHERE r.id=NEW.record_id AND p.machine_id=q.machine_id AND p.parent_uid=c.id AND c.machine_id=q.machine_id AND EXISTS(SELECT 1 FROM jsonb_array_elements(q.scope#>'{expectation,ports}') expected WHERE expected->>'physical_port_id'=p.id AND expected->>'component_id'=c.id AND expected->>'permanent_mac'=NEW.observation->>'permanent_mac')) THEN RAISE EXCEPTION 'tentative port observation must match frozen BOM identities'; END IF;
 ELSIF TG_TABLE_NAME='validation_gate_evidence' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_precollect_requests q JOIN validation_precollect_records r ON r.id=q.id JOIN exec_executions x ON x.id=q.execution_id JOIN exec_task_attempts a ON a.id=r.attempt_id JOIN exec_runner_receipts receipt ON receipt.attempt_id=a.id AND receipt.command_id=r.command_id AND receipt.fencing_token=r.fencing_token WHERE q.id=NEW.precollect_id AND q.project_id=NEW.project_id AND q.asset_id=NEW.asset_id AND q.machine_id=NEW.machine_id AND q.scope_hash=NEW.scope_hash AND x.state IN('success','failed') AND a.state='success' AND receipt.result=a.state AND x.desired_state='running' AND receipt.payload#>>'{output,precollect_record_id}'=r.id AND receipt.payload#>>'{output,evidence_hash}'=r.raw_sha256 AND receipt.payload#>>'{output,stopped}'='true' AND NEW.evaluation->>'verdict'=NEW.verdict AND NEW.evaluation->>'rule_version'=NEW.rule_version AND (NEW.verdict<>'PASS' OR (r.origin='real' AND NEW.evaluation->>'completeness_percent'='100' AND NEW.valid_until>statement_timestamp()))) THEN RAISE EXCEPTION 'G1 requires settled original stopped receipts and complete real evidence'; END IF;
  IF NEW.verdict='WAIVED' THEN RAISE EXCEPTION 'explicit scoped waiver ledger is required before waiver support'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_precollect_request_scope BEFORE INSERT ON validation_precollect_requests FOR EACH ROW EXECUTE FUNCTION keel_validation_precollect_scope();
CREATE TRIGGER validation_precollect_record_scope BEFORE INSERT ON validation_precollect_records FOR EACH ROW EXECUTE FUNCTION keel_validation_precollect_scope();
CREATE TRIGGER validation_precollect_port_scope BEFORE INSERT ON validation_precollect_ports FOR EACH ROW EXECUTE FUNCTION keel_validation_precollect_scope();
CREATE TRIGGER validation_gate_scope BEFORE INSERT ON validation_gate_evidence FOR EACH ROW EXECUTE FUNCTION keel_validation_precollect_scope();
