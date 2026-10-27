-- Publish observations only from a stopped original shared Execution.
ALTER TABLE validation_wiring_step_reports ADD COLUMN valid_until timestamptz, ADD COLUMN policy_hash char(64);
ALTER TABLE validation_wiring_step_reports ADD CONSTRAINT validation_wiring_report_freshness CHECK(step=1 OR (valid_until IS NOT NULL AND policy_hash ~ '^[0-9a-f]{64}$') IS TRUE);
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
   IF NEW.step NOT IN(2,3) THEN RAISE EXCEPTION 'Step 4 requires explicit DL confirmation'; END IF;
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
CREATE UNIQUE INDEX validation_wiring_command_step ON validation_wiring_step_reports((detail->>'command_id'),step) WHERE step IN(2,3);
