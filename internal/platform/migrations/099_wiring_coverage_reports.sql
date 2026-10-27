-- Full-denominator reports preserve the provenance and TTL of EVERY port.
CREATE OR REPLACE FUNCTION keel_wiring_step_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE host jsonb; frozen_hash text; expected_ports jsonb;
BEGIN

 IF TG_TABLE_NAME='validation_wiring_step_reports' THEN
 IF NEW.step IN(2,3) AND NEW.detail ? 'coverage_id' THEN
  IF NEW.version<>coalesce((SELECT max(version)+1 FROM validation_wiring_step_reports WHERE batch_id=NEW.batch_id AND asset_id=NEW.asset_id AND step=NEW.step),1) THEN RAISE EXCEPTION 'coverage report version must be monotonic'; END IF;
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports p JOIN validation_wiring_coverage_snapshots coverage ON coverage.id=NEW.detail->>'coverage_id' JOIN validation_wiring_commands c ON c.command_id=coverage.command_id JOIN validation_wiring_capture_records original ON original.command_id=c.command_id WHERE p.id=NEW.parent_id AND p.batch_id=NEW.batch_id AND p.asset_id=NEW.asset_id AND p.step=NEW.step-1 AND p.verdict='PASS' AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=p.batch_id AND newer.asset_id=p.asset_id AND newer.step=p.step AND newer.version>p.version) AND coverage.batch_id=NEW.batch_id AND coverage.asset_id=NEW.asset_id AND coverage.command_id=NEW.detail->>'command_id' AND coverage.source_hash=NEW.source_hash AND coverage.created_by=NEW.created_by AND NEW.verdict=coverage.coverage->>('step'||NEW.step::text) AND NEW.detail->'findings'=coverage.coverage->'findings' AND NEW.detail->>'raw_sha256'=original.raw_sha256 AND NEW.detail->>'host_origin'=c.command->>'host_origin' AND NEW.detail->>'switch_origin'=c.origin AND NEW.policy_hash=coverage.coverage->>'policy_hash' AND NEW.valid_until<=keel_wiring_original_timestamp(coverage.coverage->>'valid_until') AND ((NEW.step=2 AND p.id=coverage.parent_id) OR (NEW.step=3 AND p.detail->>'coverage_id'=coverage.id))) THEN RAISE EXCEPTION 'aggregate StepReport requires original complete coverage and its current predecessor'; END IF;
  RETURN NEW;
 END IF;
 END IF;
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
CREATE OR REPLACE FUNCTION keel_wiring_frozen_policy() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE frozen jsonb; stopped timestamptz;
BEGIN
 IF TG_TABLE_NAME='validation_wiring_step_reports' THEN
 IF NEW.step IN(2,3) AND NEW.detail ? 'coverage_id' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_coverage_snapshots c WHERE c.id=NEW.detail->>'coverage_id' AND c.source_hash=NEW.source_hash AND NEW.policy_hash=c.coverage->>'policy_hash' AND NEW.valid_until<=keel_wiring_original_timestamp(c.coverage->>'valid_until')) THEN RAISE EXCEPTION 'aggregate report cannot renew any original port TTL'; END IF;
  RETURN NEW;
 END IF;
 END IF;
 IF TG_TABLE_NAME='validation_wiring_commands' THEN
  IF NOT ((NEW.command->>'gate_ttl_seconds')::bigint BETWEEN 1 AND 86400 AND NEW.command->>'policy_hash' ~ '^[0-9a-f]{64}$') IS TRUE THEN RAISE EXCEPTION 'registered wiring command requires a frozen gate policy'; END IF;
  SELECT r.scope INTO frozen FROM validation_wiring_run_targets r WHERE r.execution_id=NEW.execution_id AND r.asset_id=NEW.asset_id;
  IF frozen IS NOT NULL AND (NEW.command->>'policy_hash' IS DISTINCT FROM frozen->>'policy_hash' OR NEW.command->>'gate_ttl_seconds' IS DISTINCT FROM frozen->>'gate_ttl_seconds') THEN RAISE EXCEPTION 'dispatch policy differs from immutable admission'; END IF;
 ELSIF NEW.step IN(2,3) THEN
  SELECT c.command,p.stopped_at INTO frozen,stopped FROM validation_wiring_commands c JOIN validation_wiring_capture_records p ON p.command_id=c.command_id WHERE c.command_id=NEW.detail->>'command_id';
  IF frozen IS NULL OR NEW.policy_hash IS DISTINCT FROM frozen->>'policy_hash' OR NEW.valid_until>stopped+make_interval(secs=>(frozen->>'gate_ttl_seconds')::int) THEN RAISE EXCEPTION 'report cannot renew the original observation gate TTL'; END IF;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION keel_wiring_confirmation_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='validation_wiring_confirmations' THEN
 IF EXISTS(SELECT 1 FROM validation_wiring_step_reports r WHERE r.id=NEW.step3_id AND r.detail ? 'coverage_id') THEN
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports r JOIN validation_wiring_step_reports p ON p.id=r.parent_id JOIN validation_wiring_coverage_snapshots c ON c.id=r.detail->>'coverage_id' JOIN validation_wiring_batch_targets target ON target.batch_id=c.batch_id AND target.asset_id=c.asset_id JOIN users u ON u.id=NEW.created_by WHERE r.id=NEW.step3_id AND r.step=3 AND r.verdict='PASS' AND p.step=2 AND p.verdict='PASS' AND p.detail->>'coverage_id'=c.id AND r.batch_id=NEW.batch_id AND r.asset_id=NEW.asset_id AND c.project_id=NEW.project_id AND c.command_id=NEW.command_id AND c.source_hash=NEW.source_hash AND c.coverage->>'scope_hash'=NEW.scope_hash AND c.coverage->>'step2'='PASS' AND c.coverage->>'step3'='PASS' AND jsonb_array_length(c.coverage->'missing_port_ids')=0 AND jsonb_array_length(c.coverage->'proofs')=jsonb_array_length(target.expectation->'ports') AND NEW.valid_until<=r.valid_until AND NEW.valid_until>clock_timestamp() AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=NEW.project_id AND m.role IN('Admin','DL'))) AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=r.batch_id AND newer.asset_id=r.asset_id AND ((newer.step=3 AND newer.version>r.version) OR (newer.step=2 AND newer.version>p.version))) AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(c.coverage->'proofs') proof WHERE proof->>'host_origin'<>'real' OR proof->>'switch_origin'<>'device' OR proof->>'step2'<>'PASS' OR proof->>'step3'<>'PASS' OR keel_wiring_original_timestamp(proof->>'valid_until')<NEW.valid_until OR NOT EXISTS(SELECT 1 FROM validation_wiring_commands original JOIN exec_task_attempts a ON a.id=original.attempt_id WHERE original.command_id=proof->>'command_id' AND a.command_id=original.command_id AND a.fencing_token=original.fencing_token AND a.state IN('success','failed')))) THEN RAISE EXCEPTION 'G2 requires DL confirmation of every original real settled physical port'; END IF;
  RETURN NEW;
 END IF;
 ELSIF TG_TABLE_NAME='validation_wiring_confirmed_ports' THEN
 IF EXISTS(SELECT 1 FROM validation_wiring_confirmations g JOIN validation_wiring_step_reports r ON r.id=g.step3_id WHERE g.id=NEW.confirmation_id AND r.detail ? 'coverage_id') THEN
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_confirmations g JOIN validation_wiring_step_reports r ON r.id=g.step3_id JOIN validation_wiring_coverage_snapshots coverage ON coverage.id=r.detail->>'coverage_id' CROSS JOIN LATERAL jsonb_array_elements(coverage.coverage->'proofs') proof JOIN validation_wiring_commands original ON original.command_id=proof->>'command_id' JOIN validation_wiring_handshakes h ON h.id=original.handshake_id JOIN physical_ports physical ON physical.id=NEW.physical_port_id JOIN physical_ports peer ON peer.id=NEW.switch_port_id WHERE g.id=NEW.confirmation_id AND proof->'expected'=NEW.expected AND proof->>'physical_port_id'=NEW.physical_port_id AND physical.machine_id=original.command#>>'{expectation,machine_id}' AND NEW.expected->>'switch_id'=peer.switch_id AND EXISTS(SELECT 1 FROM jsonb_array_elements(h.facts#>'{targets,0,ports}') p WHERE p->>'physical_port_id'=physical.id AND p->>'switch_port_id'=peer.id AND p->>'cable_id'=NEW.cable_id)) THEN RAISE EXCEPTION 'confirmed port must retain its own original physical endpoints'; END IF;
  RETURN NEW;
 END IF;
 END IF;
 IF TG_TABLE_NAME='validation_wiring_confirmations' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports r JOIN validation_wiring_commands c ON c.command_id=r.detail->>'command_id' JOIN validation_wiring_capture_records evidence ON evidence.command_id=c.command_id JOIN exec_task_attempts a ON a.id=c.attempt_id JOIN users u ON u.id=NEW.created_by WHERE r.id=NEW.step3_id AND r.step=3 AND r.verdict='PASS' AND r.batch_id=NEW.batch_id AND r.asset_id=NEW.asset_id AND c.project_id=NEW.project_id AND c.command_id=NEW.command_id AND c.command->>'host_origin'='real' AND c.origin='device' AND a.state='success' AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND r.source_hash=NEW.source_hash AND evidence.raw_sha256=NEW.source_hash AND c.command#>>'{expectation,scope_hash}'=NEW.scope_hash AND NEW.valid_until<=r.valid_until AND NEW.valid_until>clock_timestamp() AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=NEW.project_id AND m.role IN('Admin','DL'))) AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=r.batch_id AND newer.asset_id=r.asset_id AND newer.step=3 AND newer.version>r.version)) THEN RAISE EXCEPTION 'G2 requires DL confirmation of the current real settled Step 3'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_confirmations g JOIN validation_wiring_commands c ON c.command_id=g.command_id JOIN validation_wiring_handshakes h ON h.id=c.handshake_id JOIN physical_ports physical ON physical.id=NEW.physical_port_id JOIN physical_ports peer ON peer.id=NEW.switch_port_id WHERE g.id=NEW.confirmation_id AND physical.machine_id=c.command#>>'{expectation,machine_id}' AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p=NEW.expected AND p->>'physical_port_id'=physical.id AND p->>'switch_id'=peer.switch_id) AND EXISTS(SELECT 1 FROM jsonb_array_elements(h.facts#>'{targets,0,ports}') p WHERE p->>'physical_port_id'=physical.id AND p->>'switch_port_id'=peer.id AND p->>'cable_id'=NEW.cable_id)) THEN RAISE EXCEPTION 'confirmed PortModel must retain original stable physical endpoints'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION keel_wiring_confirmation_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE selected_count integer; confirmed_count integer; linked boolean;
BEGIN
 SELECT jsonb_array_length(t.expectation->'ports') INTO selected_count FROM validation_wiring_batch_targets t WHERE t.batch_id=NEW.batch_id AND t.asset_id=NEW.asset_id;
 SELECT count(*) INTO confirmed_count FROM validation_wiring_confirmed_ports WHERE confirmation_id=NEW.id;
 SELECT EXISTS(SELECT 1 FROM validation_wiring_step_reports r WHERE r.id=NEW.step4_id AND r.step=4 AND r.parent_id=NEW.step3_id AND r.verdict='PASS') INTO linked;
 IF confirmed_count IS DISTINCT FROM selected_count OR NOT linked THEN RAISE EXCEPTION 'G2 confirmation requires every frozen selected port and its Step 4'; END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION keel_wiring_issue_observation_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id JOIN validation_wiring_commands c ON c.command_id=NEW.command_id JOIN validation_wiring_capture_records evidence ON evidence.command_id=c.command_id JOIN validation_wiring_step_reports r ON r.id=NEW.report_id WHERE b.issue_id=NEW.issue_id AND i.source='g2' AND i.project_id=b.project_id AND i.category=b.category AND c.project_id=b.project_id AND c.asset_id=b.asset_id AND c.command->>'host_origin'=b.host_origin AND c.origin=b.switch_origin AND ((NOT (r.detail ? 'coverage_id') AND r.detail->>'command_id'=c.command_id) OR EXISTS(SELECT 1 FROM validation_wiring_coverage_snapshots coverage CROSS JOIN LATERAL jsonb_array_elements(coverage.coverage->'proofs') proof WHERE coverage.id=r.detail->>'coverage_id' AND proof->>'command_id'=c.command_id AND proof->>'physical_port_id'=b.physical_port_id)) AND r.batch_id=c.batch_id AND r.asset_id=c.asset_id AND r.step=(NEW.finding->>'step')::int AND (r.source_hash=evidence.raw_sha256 OR EXISTS(SELECT 1 FROM validation_wiring_coverage_snapshots coverage WHERE coverage.id=r.detail->>'coverage_id' AND coverage.source_hash=r.source_hash)) AND NEW.finding->>'verdict'='FAIL' AND NEW.finding->>'physical_port_id'=b.physical_port_id AND NEW.finding->>'category'=b.category AND NEW.observed_at=evidence.stopped_at AND EXISTS(SELECT 1 FROM jsonb_array_elements(evidence.port_evaluations->b.physical_port_id->'findings') f WHERE f=NEW.finding)) THEN RAISE EXCEPTION 'wiring issue must retain the original concrete physical-port failure'; END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION keel_wiring_retest_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id JOIN issue_events repair ON repair.issue_id=i.id AND repair.version=NEW.repair_version AND repair.action='repair' JOIN validation_wiring_commands c ON c.command_id=NEW.command_id JOIN validation_wiring_step_reports r ON r.id=NEW.report_id JOIN exec_task_attempts a ON a.id=c.attempt_id WHERE b.issue_id=NEW.issue_id AND b.project_id=NEW.project_id AND i.version=NEW.applied_issue_version AND c.project_id=b.project_id AND c.asset_id=b.asset_id AND c.command->>'host_origin'=b.host_origin AND c.origin=b.switch_origin AND c.execution_id=NEW.execution_id AND c.created_at>repair.created_at AND ((NOT (r.detail ? 'coverage_id') AND r.detail->>'command_id'=c.command_id) OR EXISTS(SELECT 1 FROM validation_wiring_coverage_snapshots coverage CROSS JOIN LATERAL jsonb_array_elements(coverage.coverage->'proofs') proof WHERE coverage.id=r.detail->>'coverage_id' AND proof->>'command_id'=c.command_id AND proof->>'physical_port_id'=b.physical_port_id)) AND r.step IN(2,3) AND r.verdict=NEW.verdict AND r.valid_until>clock_timestamp() AND a.state IN('success','failed') AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND (NEW.verdict<>'PASS' OR r.step=3) AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p->>'physical_port_id'=b.physical_port_id) AND NOT EXISTS(SELECT 1 FROM issue_events newer WHERE newer.issue_id=i.id AND newer.version>repair.version AND newer.action IN('repair','assign','reopen'))) THEN RAISE EXCEPTION 'wiring retest requires a new settled original capture after the current repair'; END IF;
 RETURN NEW;
END $$;
