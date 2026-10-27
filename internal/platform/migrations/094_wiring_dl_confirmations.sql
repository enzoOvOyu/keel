-- DL confirmation is distinct from observation and later G3 publication.
CREATE TABLE validation_wiring_confirmations(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 batch_id varchar(32) NOT NULL,
 asset_id varchar(32) NOT NULL,
 step3_id varchar(32) NOT NULL UNIQUE REFERENCES validation_wiring_step_reports(id),
 step4_id varchar(32) NOT NULL UNIQUE REFERENCES validation_wiring_step_reports(id) DEFERRABLE INITIALLY DEFERRED,
 command_id varchar(32) NOT NULL REFERENCES validation_wiring_capture_records(command_id),
 source_hash char(64) NOT NULL CHECK(source_hash ~ '^[0-9a-f]{64}$'),
 scope_hash char(64) NOT NULL CHECK(scope_hash ~ '^[0-9a-f]{64}$'),
 valid_until timestamptz NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(batch_id,asset_id) REFERENCES validation_wiring_batch_targets(batch_id,asset_id)
);
CREATE TABLE validation_wiring_confirmed_ports(
 confirmation_id varchar(32) NOT NULL REFERENCES validation_wiring_confirmations(id),
 physical_port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 switch_port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 cable_id varchar(32) NOT NULL,
 expected jsonb NOT NULL,
 PRIMARY KEY(confirmation_id,physical_port_id),
 UNIQUE(confirmation_id,switch_port_id),
 CHECK((expected->>'physical_port_id'=physical_port_id AND expected->>'cable_id'=cable_id) IS TRUE)
);
CREATE TRIGGER validation_wiring_confirmation_immutable BEFORE UPDATE OR DELETE ON validation_wiring_confirmations FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_confirmed_port_immutable BEFORE UPDATE OR DELETE ON validation_wiring_confirmed_ports FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_wiring_confirmation_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='validation_wiring_confirmations' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports r JOIN validation_wiring_commands c ON c.command_id=r.detail->>'command_id' JOIN validation_wiring_capture_records evidence ON evidence.command_id=c.command_id JOIN exec_task_attempts a ON a.id=c.attempt_id JOIN users u ON u.id=NEW.created_by WHERE r.id=NEW.step3_id AND r.step=3 AND r.verdict='PASS' AND r.batch_id=NEW.batch_id AND r.asset_id=NEW.asset_id AND c.project_id=NEW.project_id AND c.command_id=NEW.command_id AND c.command->>'host_origin'='real' AND c.origin='device' AND a.state='success' AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND r.source_hash=NEW.source_hash AND evidence.raw_sha256=NEW.source_hash AND c.command#>>'{expectation,scope_hash}'=NEW.scope_hash AND NEW.valid_until<=r.valid_until AND NEW.valid_until>clock_timestamp() AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=NEW.project_id AND m.role IN('Admin','DL'))) AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=r.batch_id AND newer.asset_id=r.asset_id AND newer.step=3 AND newer.version>r.version)) THEN RAISE EXCEPTION 'G2 requires DL confirmation of the current real settled Step 3'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_confirmations g JOIN validation_wiring_commands c ON c.command_id=g.command_id JOIN validation_wiring_handshakes h ON h.id=c.handshake_id JOIN physical_ports physical ON physical.id=NEW.physical_port_id JOIN physical_ports peer ON peer.id=NEW.switch_port_id WHERE g.id=NEW.confirmation_id AND physical.machine_id=c.command#>>'{expectation,machine_id}' AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p=NEW.expected AND p->>'physical_port_id'=physical.id AND p->>'switch_id'=peer.switch_id) AND EXISTS(SELECT 1 FROM jsonb_array_elements(h.facts#>'{targets,0,ports}') p WHERE p->>'physical_port_id'=physical.id AND p->>'switch_port_id'=peer.id AND p->>'cable_id'=NEW.cable_id)) THEN RAISE EXCEPTION 'confirmed PortModel must retain original stable physical endpoints'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_confirmation_scope BEFORE INSERT ON validation_wiring_confirmations FOR EACH ROW EXECUTE FUNCTION keel_wiring_confirmation_scope();
CREATE TRIGGER validation_wiring_confirmed_port_scope BEFORE INSERT ON validation_wiring_confirmed_ports FOR EACH ROW EXECUTE FUNCTION keel_wiring_confirmation_scope();
CREATE OR REPLACE FUNCTION keel_wiring_step_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE host jsonb; frozen_hash text; expected_ports jsonb;
BEGIN
 IF TG_TABLE_NAME='validation_wiring_batches' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_handshakes h WHERE h.id=NEW.handshake_id AND h.project_id=NEW.project_id AND h.facts_hash=NEW.facts_hash AND h.valid_until>statement_timestamp() AND NEW.created_at>=h.created_at AND NEW.created_at<h.valid_until) THEN RAISE EXCEPTION 'batch must consume a current exact project handshake'; END IF;
 ELSIF TG_TABLE_NAME='validation_wiring_batch_targets' THEN
  SELECT target,b.facts_hash INTO host,frozen_hash FROM validation_wiring_batches b JOIN validation_wiring_handshakes h ON h.id=b.handshake_id CROSS JOIN LATERAL jsonb_array_elements(h.facts->'targets') target WHERE b.id=NEW.batch_id AND b.project_id=NEW.project_id AND target->>'asset_id'=NEW.asset_id;
  IF host IS NULL OR host->>'machine_id'<>NEW.machine_id OR host->>'topology_id'<>NEW.topology_id THEN RAISE EXCEPTION 'batch target must match the frozen selected host'; END IF;
  SELECT jsonb_agg(jsonb_build_object('physical_port_id',p->>'physical_port_id','component_id',p->>'component_id','port_index',p->>'port_index','interface',p->>'interface','permanent_mac',p->>'permanent_mac','bdf',p->>'bdf','rail',p->>'rail','cable_id',p->>'cable_id','switch_id',p->>'switch_id','switch_serial',p->>'switch_serial','switch_chassis_ids',p->'switch_chassis_ids','switch_port',p->>'switch_port') ORDER BY ordinal) INTO expected_ports FROM jsonb_array_elements(host->'ports') WITH ORDINALITY AS selected(p,ordinal);
  IF NEW.expectation IS DISTINCT FROM jsonb_build_object('version','keel.wiring/v1','project_id',NEW.project_id,'asset_id',NEW.asset_id,'machine_id',NEW.machine_id,'serial',host->>'serial','hostname',host->>'hostname','scope_hash',frozen_hash,'ports',expected_ports) THEN RAISE EXCEPTION 'runner expectation must be projected from the exact frozen handshake'; END IF;
  IF NOT EXISTS(SELECT 1 FROM project_assets a JOIN pci_topology_snapshots t ON t.project_id=a.project_id AND t.asset_id=a.id JOIN validation_wiring_batches b ON b.id=NEW.batch_id WHERE a.id=NEW.asset_id AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id AND a.version=(host->>'asset_version')::bigint AND t.id=NEW.topology_id AND t.scope_hash=host->>'topology_scope_hash' AND t.verdict='PASS' AND t.origin='real' AND t.valid_until>statement_timestamp() AND NEW.expectation->>'scope_hash'=b.facts_hash AND NEW.relations->>'scope_hash'=t.scope_hash AND NEW.relations->>'g1_id'=t.g1_id AND NEW.relations->>'step1_published'='true') THEN RAISE EXCEPTION 'Step 1 consumes current independent real topology and exact frozen relations'; END IF;
 ELSIF TG_TABLE_NAME='validation_wiring_step_reports' THEN
  IF NEW.version<>coalesce((SELECT max(version)+1 FROM validation_wiring_step_reports WHERE batch_id=NEW.batch_id AND asset_id=NEW.asset_id AND step=NEW.step),1) THEN RAISE EXCEPTION 'StepReport version must be monotonic'; END IF;
  IF NEW.step=1 THEN
   IF NEW.version<>1 OR NOT EXISTS(SELECT 1 FROM validation_wiring_batch_targets t WHERE t.batch_id=NEW.batch_id AND t.asset_id=NEW.asset_id AND NEW.detail->>'topology_id'=t.topology_id AND NEW.detail->'relations'=t.relations AND NEW.verdict=t.relations->>'effective_verdict') THEN RAISE EXCEPTION 'Step 1 must retain the exact topology relation projection'; END IF;
  ELSE
   IF NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports p WHERE p.id=NEW.parent_id AND p.batch_id=NEW.batch_id AND p.asset_id=NEW.asset_id AND p.step=NEW.step-1 AND p.verdict='PASS' AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=p.batch_id AND newer.asset_id=p.asset_id AND newer.step=p.step AND newer.version>p.version)) THEN RAISE EXCEPTION 'StepReport requires the current passing predecessor in the same scope'; END IF;
   IF NEW.step=4 THEN
    IF NOT EXISTS(SELECT 1 FROM validation_wiring_confirmations g JOIN validation_wiring_step_reports p ON p.id=g.step3_id WHERE g.step4_id=NEW.id AND g.step3_id=NEW.parent_id AND g.batch_id=NEW.batch_id AND g.asset_id=NEW.asset_id AND g.source_hash=NEW.source_hash AND NEW.verdict='PASS' AND NEW.detail->>'command_id'=g.command_id AND NEW.created_by=g.created_by AND NEW.valid_until=g.valid_until AND NEW.policy_hash=p.policy_hash) THEN RAISE EXCEPTION 'Step 4 must consume its explicit DL confirmation'; END IF;
    RETURN NEW;
   END IF;
   IF NEW.step NOT IN(2,3) THEN RAISE EXCEPTION 'unsupported wiring step'; END IF;
   IF NOT EXISTS(SELECT 1 FROM validation_wiring_commands c
    JOIN validation_wiring_capture_records evidence ON evidence.command_id=c.command_id
    JOIN validation_wiring_host_proofs hostproof ON hostproof.command_id=c.command_id AND hostproof.outcome='stopped'
    JOIN exec_task_attempts a ON a.id=c.attempt_id AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token
    JOIN exec_executions x ON x.id=c.execution_id
    JOIN exec_runner_receipts receipt ON receipt.command_id=c.command_id AND receipt.attempt_id=c.attempt_id AND receipt.fencing_token=c.fencing_token
    WHERE c.command_id=NEW.detail->>'command_id' AND c.batch_id=NEW.batch_id AND c.asset_id=NEW.asset_id
    AND NEW.valid_until<=evidence.stopped_at+interval '24 hours' AND NEW.source_hash=evidence.raw_sha256 AND NEW.detail->>'raw_sha256'=evidence.raw_sha256
    AND NEW.detail->>'host_origin'=c.command->>'host_origin' AND NEW.detail->>'switch_origin'=c.origin
    AND NEW.verdict=evidence.evaluation->>('step'||NEW.step::text)
    AND NEW.detail->'findings'=evidence.evaluation->'findings'
    AND receipt.payload#>>'{output,wiring_raw_sha256}'=evidence.raw_sha256
    AND receipt.payload#>>'{output,step2}'=evidence.evaluation->>'step2'
    AND receipt.payload#>>'{output,step3}'=evidence.evaluation->>'step3'
    AND NEW.created_by=x.created_by AND a.state IN('running','unknown','success','failed')
    AND ((NEW.step=2 AND NEW.parent_id=c.parent_id) OR (NEW.step=3 AND EXISTS(SELECT 1 FROM validation_wiring_step_reports p WHERE p.id=NEW.parent_id AND p.step=2 AND p.detail->>'command_id'=c.command_id)))) THEN
     RAISE EXCEPTION 'Step 2/3 requires exact original stopped capture and common Execution receipt';
   END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION keel_wiring_confirmation_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE selected_count integer; confirmed_count integer; linked boolean;
BEGIN
 SELECT jsonb_array_length(c.command#>'{expectation,ports}') INTO selected_count FROM validation_wiring_commands c WHERE c.command_id=NEW.command_id;
 SELECT count(*) INTO confirmed_count FROM validation_wiring_confirmed_ports WHERE confirmation_id=NEW.id;
 SELECT EXISTS(SELECT 1 FROM validation_wiring_step_reports r WHERE r.id=NEW.step4_id AND r.step=4 AND r.parent_id=NEW.step3_id AND r.verdict='PASS') INTO linked;
 IF confirmed_count IS DISTINCT FROM selected_count OR NOT linked THEN RAISE EXCEPTION 'G2 confirmation requires every frozen selected port and its Step 4'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER validation_wiring_confirmation_complete AFTER INSERT ON validation_wiring_confirmations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION keel_wiring_confirmation_complete();
