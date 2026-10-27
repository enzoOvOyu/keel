-- Host process ownership must survive worker restarts independently of a switch
-- readback that is still unknown. No task/G2 result is published by these rows.
CREATE TABLE validation_wiring_host_dispatches(
 command_id varchar(32) PRIMARY KEY REFERENCES validation_wiring_commands(command_id),
 committed_at timestamptz NOT NULL
);
CREATE TABLE validation_wiring_host_proofs(
 command_id varchar(32) PRIMARY KEY REFERENCES validation_wiring_host_dispatches(command_id),
 outcome varchar(20) NOT NULL CHECK(outcome IN('local_not_started','stopped')),
 stopped_at timestamptz,
 host_evidence_hash char(64),
 raw_key text,
 raw_version text,
 raw_sha256 char(64),
 raw_size bigint,
 received_at timestamptz NOT NULL,
 CHECK((outcome='local_not_started' AND stopped_at IS NULL AND host_evidence_hash IS NULL AND raw_key IS NULL AND raw_version IS NULL AND raw_sha256 IS NULL AND raw_size IS NULL)
 OR (outcome='stopped' AND stopped_at IS NOT NULL AND host_evidence_hash ~ '^[0-9a-f]{64}$' AND length(raw_key)>0 AND length(raw_version)>0 AND raw_version<>'null' AND raw_sha256 ~ '^[0-9a-f]{64}$' AND raw_size BETWEEN 1 AND 2097152) IS TRUE)
);
ALTER TABLE validation_wiring_capture_records ADD COLUMN host_evidence_hash char(64);
CREATE TRIGGER validation_wiring_host_dispatch_immutable BEFORE UPDATE OR DELETE ON validation_wiring_host_dispatches FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_host_proof_immutable BEFORE UPDATE OR DELETE ON validation_wiring_host_proofs FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_wiring_host_session_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c validation_wiring_commands; started timestamptz; live boolean;
BEGIN
 SELECT * INTO c FROM validation_wiring_commands WHERE command_id=NEW.command_id;
 IF c.command_id IS NULL THEN RAISE EXCEPTION 'registered original wiring command required'; END IF;
 SELECT a.state='running' INTO live FROM exec_task_attempts a WHERE a.id=c.attempt_id AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND ((a.state='running' AND a.lease_until>clock_timestamp()) OR a.state='unknown');
 IF live IS NULL OR NOT keel_wiring_exclusive_scope(c.attempt_id,c.fencing_token,c.command#>>'{expectation,machine_id}',c.command#>'{expectation,ports}',live) THEN RAISE EXCEPTION 'original held or quarantined host/switch resources required'; END IF;
 IF TG_TABLE_NAME='validation_wiring_host_dispatches' THEN
  IF NOT live OR NOT EXISTS(SELECT 1 FROM validation_wiring_handshakes h JOIN exec_executions x ON x.id=c.execution_id WHERE h.id=c.handshake_id AND h.valid_until>clock_timestamp() AND x.desired_state='running') THEN RAISE EXCEPTION 'new host start requires a live attempt and current dispatch readiness'; END IF;
 ELSE
  SELECT committed_at INTO started FROM validation_wiring_host_dispatches WHERE command_id=c.command_id;
  IF NEW.outcome='local_not_started' THEN
   IF EXISTS(SELECT 1 FROM validation_wiring_readback_windows WHERE command_id=c.command_id) OR EXISTS(SELECT 1 FROM validation_wiring_readback_dispatches WHERE command_id=c.command_id) THEN RAISE EXCEPTION 'a dispatched collector cannot claim host work never started'; END IF;
  ELSE
   IF NEW.stopped_at<started OR NEW.stopped_at>clock_timestamp()+interval '5 seconds' OR NEW.raw_key IS DISTINCT FROM 'validation/wiring-host/'||c.project_id||'/'||c.asset_id||'/'||c.command_id||'/'||NEW.raw_sha256||'.json' THEN RAISE EXCEPTION 'original positive host stop archive required'; END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_host_dispatch_scope BEFORE INSERT ON validation_wiring_host_dispatches FOR EACH ROW EXECUTE FUNCTION keel_wiring_host_session_scope();
CREATE TRIGGER validation_wiring_host_proof_scope BEFORE INSERT ON validation_wiring_host_proofs FOR EACH ROW EXECUTE FUNCTION keel_wiring_host_session_scope();
CREATE FUNCTION keel_wiring_capture_host_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM validation_wiring_host_proofs p WHERE p.command_id=NEW.command_id AND p.outcome='stopped' AND p.stopped_at=NEW.stopped_at AND NEW.host_evidence_hash=p.host_evidence_hash) THEN RAISE EXCEPTION 'full capture must preserve the original independent host stop proof'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_capture_host_binding BEFORE INSERT ON validation_wiring_capture_records FOR EACH ROW EXECUTE FUNCTION keel_wiring_capture_host_binding();
CREATE FUNCTION keel_wiring_terminal_consistency() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='validation_wiring_readback_queries' THEN
  IF EXISTS(SELECT 1 FROM validation_wiring_readback_results r WHERE r.child_command_id=NEW.child_command_id AND (NEW.observation->>'state' IS DISTINCT FROM 'completed' OR NEW.observation->'result' IS DISTINCT FROM r.response)) THEN RAISE EXCEPTION 'original terminal query contradicts retained completion'; END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM validation_wiring_readback_queries q WHERE q.child_command_id=NEW.child_command_id AND (q.observation->>'state' IS DISTINCT FROM 'completed' OR q.observation->'result' IS DISTINCT FROM NEW.response)) THEN RAISE EXCEPTION 'completion contradicts original terminal query'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_query_consistent BEFORE INSERT ON validation_wiring_readback_queries FOR EACH ROW EXECUTE FUNCTION keel_wiring_terminal_consistency();
CREATE TRIGGER validation_wiring_result_consistent BEFORE INSERT ON validation_wiring_readback_results FOR EACH ROW EXECUTE FUNCTION keel_wiring_terminal_consistency();
