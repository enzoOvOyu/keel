-- Partial reruns keep the original full denominator and exact stable identities.
CREATE FUNCTION keel_wiring_expectation_within(full_scope jsonb, selected jsonb) RETURNS boolean LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT (full_scope-'ports')=(selected-'ports') AND jsonb_typeof(selected->'ports')='array' AND jsonb_array_length(selected->'ports') BETWEEN 1 AND jsonb_array_length(full_scope->'ports') AND (SELECT count(DISTINCT p->>'physical_port_id') FROM jsonb_array_elements(selected->'ports') p)=jsonb_array_length(selected->'ports') AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(selected->'ports') p WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(full_scope->'ports') original WHERE original=p))
$$;
CREATE FUNCTION keel_wiring_handshake_ports(facts jsonb, asset text) RETURNS jsonb LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT jsonb_agg(jsonb_build_object('physical_port_id',p->>'physical_port_id','component_id',p->>'component_id','port_index',p->>'port_index','interface',p->>'interface','permanent_mac',p->>'permanent_mac','bdf',p->>'bdf','rail',p->>'rail','cable_id',p->>'cable_id','switch_id',p->>'switch_id','switch_serial',p->>'switch_serial','switch_chassis_ids',p->'switch_chassis_ids','switch_port',p->>'switch_port') ORDER BY ordinal) FROM jsonb_array_elements(facts->'targets') target CROSS JOIN LATERAL jsonb_array_elements(target->'ports') WITH ORDINALITY AS selected(p,ordinal) WHERE target->>'asset_id'=asset
$$;
CREATE OR REPLACE FUNCTION keel_wiring_run_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='validation_wiring_runs' THEN
  IF NOT EXISTS(SELECT 1 FROM exec_executions x JOIN validation_wiring_batches b ON b.id=NEW.batch_id JOIN validation_wiring_handshakes h ON h.id=NEW.handshake_id WHERE x.id=NEW.execution_id AND x.project_id=NEW.project_id AND b.project_id=x.project_id AND h.project_id=x.project_id AND x.created_by=NEW.created_by AND x.mode='fabric' AND x.parameters->>'validation_kind'='wiring' AND x.parameters->>'wiring_batch_id'=b.id AND h.valid_until>clock_timestamp() AND h.collector->>'state'='ready') THEN RAISE EXCEPTION 'wiring admission must retain current scoped readiness and its Execution owner'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_runs r JOIN validation_wiring_batch_targets t ON t.batch_id=r.batch_id JOIN validation_wiring_step_reports p ON p.batch_id=t.batch_id AND p.asset_id=t.asset_id AND p.step=1 JOIN validation_wiring_handshakes h ON h.id=r.handshake_id JOIN exec_execution_targets et ON et.execution_id=r.execution_id JOIN exec_targets target ON target.id=et.target_id WHERE r.execution_id=NEW.execution_id AND t.asset_id=NEW.asset_id AND t.machine_id=NEW.machine_id AND NEW.scope->>'project_id'=r.project_id AND NEW.scope->>'batch_id'=r.batch_id AND NEW.scope->>'parent_id'=p.id AND p.verdict='PASS' AND NEW.scope->>'topology_id'=t.topology_id AND keel_wiring_expectation_within(t.expectation,NEW.scope->'expectation') AND NEW.scope#>'{expectation,ports}'=keel_wiring_handshake_ports(h.facts,t.asset_id) AND target.kind='machine' AND target.resource_key=t.machine_id AND EXISTS(SELECT 1 FROM jsonb_array_elements(h.facts->'targets') v WHERE v->>'asset_id'=t.asset_id AND v->>'topology_id'=t.topology_id) AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=p.batch_id AND newer.asset_id=p.asset_id AND newer.step=1 AND newer.version>p.version)) THEN RAISE EXCEPTION 'wiring request must freeze its exact Step 1 and admitted host'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION keel_wiring_command_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM validation_wiring_batch_targets t
  JOIN validation_wiring_step_reports p ON p.batch_id=t.batch_id AND p.asset_id=t.asset_id
  JOIN validation_wiring_handshakes h ON h.id=NEW.handshake_id
  JOIN exec_task_attempts a ON a.id=NEW.attempt_id
  JOIN exec_targets target ON target.id=a.target_id
  JOIN exec_executions x ON x.id=a.execution_id
  WHERE t.batch_id=NEW.batch_id AND t.asset_id=NEW.asset_id AND t.project_id=NEW.project_id
  AND p.id=NEW.parent_id AND p.step=1 AND p.verdict='PASS'
  AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports n WHERE n.batch_id=p.batch_id AND n.asset_id=p.asset_id AND n.step=1 AND n.version>p.version)
  AND keel_wiring_expectation_within(t.expectation,NEW.command->'expectation') AND NEW.command#>'{expectation,ports}'=keel_wiring_handshake_ports(h.facts,t.asset_id) AND (NEW.command->'expectation'=t.expectation OR EXISTS(SELECT 1 FROM validation_wiring_run_targets selected WHERE selected.execution_id=NEW.execution_id AND selected.asset_id=NEW.asset_id AND selected.scope->'expectation'=NEW.command->'expectation' AND selected.scope->>'parent_id'=NEW.parent_id)) AND NEW.command#>>'{plan,version}'='keel.wiring/v1' AND NEW.command#>>'{plan,scope_hash}'=t.expectation->>'scope_hash'
  AND h.project_id=NEW.project_id AND h.valid_until>clock_timestamp() AND h.collector->>'state'='ready'
  AND jsonb_array_length(h.facts->'targets')=1 AND h.facts#>>'{targets,0,asset_id}'=t.asset_id AND h.facts#>>'{targets,0,topology_id}'=t.topology_id
  AND a.command_id=NEW.command_id AND a.execution_id=NEW.execution_id AND a.fencing_token=NEW.fencing_token AND a.state='running' AND a.lease_until>clock_timestamp()
  AND target.kind='machine' AND target.resource_key=t.machine_id AND x.project_id=t.project_id AND x.mode='fabric' AND x.desired_state='running' AND x.parameters->>'validation_kind'='wiring'
  AND keel_wiring_exclusive_scope(a.id,a.fencing_token,t.machine_id,NEW.command#>'{expectation,ports}',true)) THEN
  RAISE EXCEPTION 'LLDP start requires current Step 1, a new readiness lease and complete shared exclusive resources';
 END IF;
 RETURN NEW;
END $$;
