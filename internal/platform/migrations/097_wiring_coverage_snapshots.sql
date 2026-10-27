-- Durable full-denominator coverage assembled from original per-port evidence.
-- This table alone does not enable partial commands or publish aggregate G2.
CREATE TABLE validation_wiring_port_evidence(
 id varchar(32) PRIMARY KEY,
 command_id varchar(32) NOT NULL REFERENCES validation_wiring_capture_records(command_id),
 physical_port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 parent_id varchar(32) NOT NULL REFERENCES validation_wiring_step_reports(id),
 proof jsonb NOT NULL,
 observed_at timestamptz NOT NULL,
 valid_until timestamptz NOT NULL,
 UNIQUE(command_id,physical_port_id),
 CHECK((proof->>'physical_port_id'=physical_port_id AND proof->>'command_id'=command_id AND proof->>'parent_id'=parent_id AND proof->>'step2_id'=id AND (proof->>'observed_at')::timestamptz=observed_at AND (proof->>'valid_until')::timestamptz=valid_until) IS TRUE)
);
CREATE TABLE validation_wiring_coverage_snapshots(
 id varchar(32) PRIMARY KEY,
 command_id varchar(32) NOT NULL UNIQUE REFERENCES validation_wiring_commands(command_id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 batch_id varchar(32) NOT NULL,
 asset_id varchar(32) NOT NULL,
 parent_id varchar(32) NOT NULL REFERENCES validation_wiring_step_reports(id),
 source_hash char(64) NOT NULL CHECK(source_hash ~ '^[0-9a-f]{64}$'),
 coverage jsonb NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(batch_id,asset_id) REFERENCES validation_wiring_batch_targets(batch_id,asset_id),
 CHECK((coverage->>'version'='keel.wiring-coverage/v1' AND coverage->>'parent_id'=parent_id AND coverage->>'source_hash'=source_hash AND coverage->>'step2' IN('PASS','FAIL','BLOCKED') AND coverage->>'step3' IN('PASS','FAIL','BLOCKED') AND (coverage->>'step2'='PASS' OR coverage->>'step3'='BLOCKED')) IS TRUE)
);
CREATE TRIGGER validation_wiring_port_evidence_immutable BEFORE UPDATE OR DELETE ON validation_wiring_port_evidence FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_coverage_immutable BEFORE UPDATE OR DELETE ON validation_wiring_coverage_snapshots FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_wiring_coverage_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c validation_wiring_commands; evidence validation_wiring_capture_records; selected jsonb; port jsonb; present integer; missing integer;
BEGIN
 SELECT * INTO c FROM validation_wiring_commands WHERE command_id=NEW.command_id;
 SELECT * INTO evidence FROM validation_wiring_capture_records WHERE command_id=NEW.command_id;
 IF c.command_id IS NULL OR evidence.command_id IS NULL OR NOT EXISTS(SELECT 1 FROM exec_runner_receipts r WHERE r.command_id=c.command_id AND r.attempt_id=c.attempt_id AND r.fencing_token=c.fencing_token AND r.payload#>>'{output,wiring_raw_sha256}'=evidence.raw_sha256) THEN RAISE EXCEPTION 'coverage requires the retained original common receipt'; END IF;
 IF TG_TABLE_NAME='validation_wiring_port_evidence' THEN
  IF NEW.parent_id IS DISTINCT FROM c.parent_id OR NEW.observed_at IS DISTINCT FROM evidence.stopped_at OR NEW.valid_until>evidence.stopped_at+make_interval(secs=>(c.command->>'gate_ttl_seconds')::int) OR NOT (NEW.proof->>'scope_hash'=c.command#>>'{expectation,scope_hash}' AND NEW.proof->>'policy_hash'=c.command->>'policy_hash' AND NEW.proof->>'host_origin'=c.command->>'host_origin' AND NEW.proof->>'switch_origin'=c.origin AND NEW.proof->>'raw_sha256'=evidence.raw_sha256 AND NEW.proof->>'step2'=evidence.port_evaluations->NEW.physical_port_id->>'step2' AND NEW.proof->>'step3'=evidence.port_evaluations->NEW.physical_port_id->>'step3' AND NEW.proof->'findings'=evidence.port_evaluations->NEW.physical_port_id->'findings') IS TRUE OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p=NEW.proof->'expected' AND p->>'physical_port_id'=NEW.physical_port_id) THEN RAISE EXCEPTION 'port evidence must retain the exact original port rule projection'; END IF;
 ELSE
  SELECT t.expectation INTO selected FROM validation_wiring_batch_targets t WHERE t.batch_id=NEW.batch_id AND t.asset_id=NEW.asset_id AND t.project_id=NEW.project_id;
  IF selected IS NULL OR NEW.project_id IS DISTINCT FROM c.project_id OR NEW.batch_id IS DISTINCT FROM c.batch_id OR NEW.asset_id IS DISTINCT FROM c.asset_id OR NEW.parent_id IS DISTINCT FROM c.parent_id OR NEW.coverage->>'scope_hash' IS DISTINCT FROM selected->>'scope_hash' THEN RAISE EXCEPTION 'coverage must retain its entire Step 1 denominator'; END IF;
  present:=jsonb_array_length(NEW.coverage->'proofs');missing:=jsonb_array_length(NEW.coverage->'missing_port_ids');
  IF present+missing<>jsonb_array_length(selected->'ports') OR (SELECT count(DISTINCT id) FROM (SELECT p->>'physical_port_id' AS id FROM jsonb_array_elements(NEW.coverage->'proofs') p UNION ALL SELECT jsonb_array_elements_text(NEW.coverage->'missing_port_ids')) ids)<>jsonb_array_length(selected->'ports') THEN RAISE EXCEPTION 'coverage cannot shrink or duplicate the denominator'; END IF;
  IF missing>0 AND NEW.coverage->>'step2'='PASS' THEN RAISE EXCEPTION 'missing ports cannot pass coverage'; END IF;
  IF EXISTS(SELECT 1 FROM jsonb_array_elements_text(NEW.coverage->'missing_port_ids') id WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(selected->'ports') expected WHERE expected->>'physical_port_id'=id)) THEN RAISE EXCEPTION 'missing ports must retain the frozen denominator'; END IF;
  FOR port IN SELECT value FROM jsonb_array_elements(NEW.coverage->'proofs') LOOP
   IF NOT EXISTS(SELECT 1 FROM validation_wiring_port_evidence p JOIN validation_wiring_commands original ON original.command_id=p.command_id WHERE p.id=port->>'step2_id' AND p.parent_id=NEW.parent_id AND p.proof=port AND original.batch_id=c.batch_id AND original.asset_id=c.asset_id AND original.command->>'host_origin'=c.command->>'host_origin' AND original.origin=c.origin AND EXISTS(SELECT 1 FROM jsonb_array_elements(selected->'ports') expected WHERE expected=port->'expected') AND NOT EXISTS(SELECT 1 FROM validation_wiring_port_evidence newer JOIN validation_wiring_commands source ON source.command_id=newer.command_id WHERE newer.parent_id=p.parent_id AND newer.physical_port_id=p.physical_port_id AND source.batch_id=c.batch_id AND source.asset_id=c.asset_id AND source.command->>'host_origin'=c.command->>'host_origin' AND source.origin=c.origin AND (newer.observed_at,newer.command_id)>(p.observed_at,p.command_id))) THEN RAISE EXCEPTION 'coverage must use the latest original proof for each physical port'; END IF;
  END LOOP;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_port_evidence_scope BEFORE INSERT ON validation_wiring_port_evidence FOR EACH ROW EXECUTE FUNCTION keel_wiring_coverage_scope();
CREATE TRIGGER validation_wiring_coverage_scope BEFORE INSERT ON validation_wiring_coverage_snapshots FOR EACH ROW EXECUTE FUNCTION keel_wiring_coverage_scope();
