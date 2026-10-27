-- Positive local nonstart tombstones, never inferred from a remote 404.
CREATE TABLE validation_wiring_nonstarts(
 command_id varchar(32) PRIMARY KEY REFERENCES exec_task_attempts(command_id),
 attempt_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 code varchar(80) NOT NULL CHECK(code ~ '^[A-Z][A-Z0-9_]{2,79}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER validation_wiring_nonstart_immutable BEFORE UPDATE OR DELETE ON validation_wiring_nonstarts FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_wiring_nonstart_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE original exec_task_attempts; selected jsonb; live boolean;
BEGIN
 IF TG_TABLE_NAME='validation_wiring_nonstarts' THEN
  SELECT * INTO original FROM exec_task_attempts WHERE id=NEW.attempt_id FOR UPDATE;
  IF original.command_id IS DISTINCT FROM NEW.command_id OR original.execution_id IS DISTINCT FROM NEW.execution_id OR original.fencing_token IS DISTINCT FROM NEW.fencing_token THEN RAISE EXCEPTION 'nonstart must match original fenced attempt'; END IF;
  SELECT r.scope INTO selected FROM validation_wiring_run_targets r JOIN exec_targets t ON t.resource_key=r.machine_id AND t.id=original.target_id JOIN exec_executions x ON x.id=r.execution_id WHERE r.execution_id=original.execution_id AND x.parameters->>'validation_kind'='wiring';
  live:=original.state='running' AND original.lease_until>clock_timestamp();
  IF selected IS NULL OR NOT (live OR original.state='unknown') OR NOT keel_wiring_exclusive_scope(original.id,original.fencing_token,selected->>'machine_id',selected#>'{expectation,ports}',live) THEN RAISE EXCEPTION 'original host and switch ownership required for local nonstart'; END IF;
  IF EXISTS(SELECT 1 FROM validation_wiring_host_dispatches d WHERE d.command_id=NEW.command_id) AND NOT EXISTS(SELECT 1 FROM validation_wiring_host_proofs p WHERE p.command_id=NEW.command_id AND p.outcome='local_not_started') THEN RAISE EXCEPTION 'a start intent without positive nonstart proof is UNKNOWN'; END IF;
  IF EXISTS(SELECT 1 FROM validation_wiring_readback_dispatches WHERE command_id=NEW.command_id) THEN RAISE EXCEPTION 'collector dispatch contradicts a local nonstart'; END IF;
 ELSE
  SELECT * INTO original FROM exec_task_attempts WHERE command_id=NEW.command_id FOR UPDATE;
  IF EXISTS(SELECT 1 FROM validation_wiring_nonstarts WHERE command_id=NEW.command_id) THEN RAISE EXCEPTION 'original command has a terminal local nonstart tombstone'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_nonstart_scope BEFORE INSERT ON validation_wiring_nonstarts FOR EACH ROW EXECUTE FUNCTION keel_wiring_nonstart_scope();
CREATE TRIGGER validation_wiring_command_nonstart_barrier BEFORE INSERT ON validation_wiring_commands FOR EACH ROW EXECUTE FUNCTION keel_wiring_nonstart_scope();
CREATE TRIGGER validation_wiring_host_nonstart_barrier BEFORE INSERT ON validation_wiring_host_dispatches FOR EACH ROW EXECUTE FUNCTION keel_wiring_nonstart_scope();
