-- Physical failures retain the original host probe and approved policy.
ALTER TABLE validation_wiring_issue_bindings DROP CONSTRAINT validation_wiring_issue_bindings_category_check;
ALTER TABLE validation_wiring_issue_bindings ADD CONSTRAINT validation_wiring_issue_bindings_category_check CHECK(category IN('T1','T2','T3','T4','T5','T6'));

CREATE FUNCTION keel_wiring_health_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE policy jsonb; frozen jsonb;
BEGIN
 IF TG_TABLE_NAME='validation_wiring_batch_targets' THEN
  policy:=NEW.expectation->'health_policy';
  SELECT h.facts->'health_policy' INTO frozen FROM validation_wiring_batches b JOIN validation_wiring_handshakes h ON h.id=b.handshake_id WHERE b.id=NEW.batch_id AND b.project_id=NEW.project_id;
 ELSIF TG_TABLE_NAME='validation_wiring_run_targets' THEN
  policy:=NEW.scope#>'{expectation,health_policy}';
  SELECT h.facts->'health_policy' INTO frozen FROM validation_wiring_runs r JOIN validation_wiring_handshakes h ON h.id=r.handshake_id WHERE r.execution_id=NEW.execution_id;
 ELSE
  policy:=NEW.command#>'{expectation,health_policy}';
  SELECT h.facts->'health_policy' INTO frozen FROM validation_wiring_handshakes h WHERE h.id=NEW.handshake_id AND h.project_id=NEW.project_id;
 END IF;
 IF NOT (policy->>'version'='keel.wiring-health/v1' AND policy=frozen AND jsonb_typeof(policy->'counters')='object' AND jsonb_typeof(policy->'optics')='object') IS TRUE THEN RAISE EXCEPTION 'new wiring scope requires the same frozen approved diagnostic policy'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_health_target BEFORE INSERT ON validation_wiring_batch_targets FOR EACH ROW EXECUTE FUNCTION keel_wiring_health_scope();
CREATE TRIGGER validation_wiring_health_run BEFORE INSERT ON validation_wiring_run_targets FOR EACH ROW EXECUTE FUNCTION keel_wiring_health_scope();
CREATE TRIGGER validation_wiring_health_command BEFORE INSERT ON validation_wiring_commands FOR EACH ROW EXECUTE FUNCTION keel_wiring_health_scope();

CREATE OR REPLACE FUNCTION keel_wiring_retest_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id JOIN issue_events repair ON repair.issue_id=i.id AND repair.version=NEW.repair_version AND repair.action='repair' JOIN validation_wiring_commands c ON c.command_id=NEW.command_id JOIN validation_wiring_step_reports r ON r.id=NEW.report_id JOIN exec_task_attempts a ON a.id=c.attempt_id WHERE b.issue_id=NEW.issue_id AND b.project_id=NEW.project_id AND i.version=NEW.applied_issue_version AND c.project_id=b.project_id AND c.asset_id=b.asset_id AND c.command->>'host_origin'=b.host_origin AND c.origin=b.switch_origin AND c.execution_id=NEW.execution_id AND c.created_at>repair.created_at AND ((NOT (r.detail ? 'coverage_id') AND r.detail->>'command_id'=c.command_id) OR EXISTS(SELECT 1 FROM validation_wiring_coverage_snapshots coverage CROSS JOIN LATERAL jsonb_array_elements(coverage.coverage->'proofs') proof WHERE coverage.id=r.detail->>'coverage_id' AND proof->>'command_id'=c.command_id AND proof->>'physical_port_id'=b.physical_port_id)) AND (b.category NOT IN('T3','T4','T5') OR c.command#>>'{expectation,health_policy,version}'='keel.wiring-health/v1') AND (NEW.verdict<>'FAIL' OR EXISTS(SELECT 1 FROM validation_wiring_port_evidence evidence CROSS JOIN LATERAL jsonb_array_elements(evidence.proof->'findings') finding WHERE evidence.command_id=c.command_id AND evidence.physical_port_id=b.physical_port_id AND finding->>'verdict'='FAIL' AND finding->>'category'=b.category AND finding->>'physical_port_id'=b.physical_port_id)) AND (NEW.verdict<>'PASS' OR b.category NOT IN('T3','T4','T5') OR (EXISTS(SELECT 1 FROM validation_wiring_port_evidence evidence CROSS JOIN LATERAL jsonb_array_elements(evidence.proof->'health_checks') health WHERE evidence.command_id=c.command_id AND evidence.physical_port_id=b.physical_port_id AND health->>'category'=b.category AND health->>'verdict'='PASS') AND NOT EXISTS(SELECT 1 FROM validation_wiring_port_evidence evidence CROSS JOIN LATERAL jsonb_array_elements(evidence.proof->'health_checks') health WHERE evidence.command_id=c.command_id AND evidence.physical_port_id=b.physical_port_id AND health->>'category'=b.category AND (health->>'verdict'='PASS') IS NOT TRUE))) AND r.step IN(2,3) AND r.verdict=NEW.verdict AND r.valid_until>clock_timestamp() AND a.state IN('success','failed') AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND (NEW.verdict<>'PASS' OR r.step=3) AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p->>'physical_port_id'=b.physical_port_id) AND NOT EXISTS(SELECT 1 FROM issue_events newer WHERE newer.issue_id=i.id AND newer.version>repair.version AND newer.action IN('repair','assign','reopen'))) THEN RAISE EXCEPTION 'wiring retest requires a new settled original capture after the current repair'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION keel_wiring_health_proof() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.proof->'health_checks' IS DISTINCT FROM (SELECT evidence.port_evaluations->NEW.physical_port_id->'health_checks' FROM validation_wiring_capture_records evidence WHERE evidence.command_id=NEW.command_id) THEN RAISE EXCEPTION 'health checks must retain the exact original port rule projection'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_health_proof BEFORE INSERT ON validation_wiring_port_evidence FOR EACH ROW EXECUTE FUNCTION keel_wiring_health_proof();
