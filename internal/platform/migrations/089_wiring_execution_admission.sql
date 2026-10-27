-- A domain-owned shared Execution; admission never reuses its lease at dispatch.
CREATE TABLE validation_wiring_runs(
 execution_id varchar(32) PRIMARY KEY REFERENCES exec_executions(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 batch_id varchar(32) NOT NULL REFERENCES validation_wiring_batches(id),
 handshake_id varchar(32) NOT NULL UNIQUE REFERENCES validation_wiring_handshakes(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE validation_wiring_run_targets(
 execution_id varchar(32) NOT NULL REFERENCES validation_wiring_runs(execution_id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 scope jsonb NOT NULL,
 PRIMARY KEY(execution_id,asset_id),
 UNIQUE(execution_id,machine_id),
 CHECK((scope->>'asset_id'=asset_id AND scope->>'machine_id'=machine_id AND scope->>'identity_hash' ~ '^[0-9a-f]{64}$' AND scope#>>'{expectation,asset_id}'=asset_id AND scope#>>'{expectation,machine_id}'=machine_id) IS TRUE)
);
CREATE TRIGGER validation_wiring_run_immutable BEFORE UPDATE OR DELETE ON validation_wiring_runs FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_run_target_immutable BEFORE UPDATE OR DELETE ON validation_wiring_run_targets FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE OR REPLACE FUNCTION keel_wiring_handshake_claim() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM validation_wiring_handshakes WHERE id=NEW.handshake_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM validation_wiring_batches WHERE handshake_id=NEW.handshake_id)
 OR EXISTS(SELECT 1 FROM validation_wiring_commands WHERE handshake_id=NEW.handshake_id)
 OR EXISTS(SELECT 1 FROM validation_wiring_runs WHERE handshake_id=NEW.handshake_id) THEN
  RAISE EXCEPTION 'wiring readiness handshake is already consumed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_run_handshake_claim BEFORE INSERT ON validation_wiring_runs FOR EACH ROW EXECUTE FUNCTION keel_wiring_handshake_claim();
CREATE FUNCTION keel_wiring_run_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='validation_wiring_runs' THEN
  IF NOT EXISTS(SELECT 1 FROM exec_executions x JOIN validation_wiring_batches b ON b.id=NEW.batch_id JOIN validation_wiring_handshakes h ON h.id=NEW.handshake_id WHERE x.id=NEW.execution_id AND x.project_id=NEW.project_id AND b.project_id=x.project_id AND h.project_id=x.project_id AND x.created_by=NEW.created_by AND x.mode='fabric' AND x.parameters->>'validation_kind'='wiring' AND x.parameters->>'wiring_batch_id'=b.id AND h.valid_until>clock_timestamp() AND h.collector->>'state'='ready') THEN RAISE EXCEPTION 'wiring admission must retain current scoped readiness and its Execution owner'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_runs r JOIN validation_wiring_batch_targets t ON t.batch_id=r.batch_id JOIN validation_wiring_step_reports p ON p.batch_id=t.batch_id AND p.asset_id=t.asset_id AND p.step=1 JOIN validation_wiring_handshakes h ON h.id=r.handshake_id JOIN exec_execution_targets et ON et.execution_id=r.execution_id JOIN exec_targets target ON target.id=et.target_id WHERE r.execution_id=NEW.execution_id AND t.asset_id=NEW.asset_id AND t.machine_id=NEW.machine_id AND NEW.scope->>'project_id'=r.project_id AND NEW.scope->>'batch_id'=r.batch_id AND NEW.scope->>'parent_id'=p.id AND p.verdict='PASS' AND NEW.scope->>'topology_id'=t.topology_id AND NEW.scope->'expectation'=t.expectation AND target.kind='machine' AND target.resource_key=t.machine_id AND EXISTS(SELECT 1 FROM jsonb_array_elements(h.facts->'targets') v WHERE v->>'asset_id'=t.asset_id AND v->>'topology_id'=t.topology_id) AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=p.batch_id AND newer.asset_id=p.asset_id AND newer.step=1 AND newer.version>p.version)) THEN RAISE EXCEPTION 'wiring request must freeze its exact Step 1 and admitted host'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_run_scope BEFORE INSERT ON validation_wiring_runs FOR EACH ROW EXECUTE FUNCTION keel_wiring_run_scope();
CREATE TRIGGER validation_wiring_run_target_scope BEFORE INSERT ON validation_wiring_run_targets FOR EACH ROW EXECUTE FUNCTION keel_wiring_run_scope();
