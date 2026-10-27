-- Observation TTL is frozen before host execution, never extended on recovery.
CREATE FUNCTION keel_wiring_frozen_policy() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE frozen jsonb; stopped timestamptz;
BEGIN
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
CREATE TRIGGER validation_wiring_command_policy BEFORE INSERT ON validation_wiring_commands FOR EACH ROW EXECUTE FUNCTION keel_wiring_frozen_policy();
CREATE TRIGGER validation_wiring_report_policy BEFORE INSERT ON validation_wiring_step_reports FOR EACH ROW EXECUTE FUNCTION keel_wiring_frozen_policy();
