-- Retain terminal original-command queries and stopped captures. These records
-- are inputs to a later settled-receipt StepReport consumer, not G2 evidence.
ALTER TABLE validation_wiring_commands ADD CONSTRAINT validation_wiring_host_origin CHECK((command->>'host_origin' IN('real','sim')) IS TRUE);
CREATE TABLE validation_wiring_readback_queries(
 child_command_id varchar(32) PRIMARY KEY REFERENCES validation_wiring_readback_dispatches(child_command_id),
 observation jsonb NOT NULL,
 content_hash char(64) NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL,
 CHECK((observation->>'version'='fabric-recollect-status/v1' AND observation->>'command_id'=child_command_id AND observation->>'state' IN('completed','stopped','rejected') AND observation->>'finished_at' IS NOT NULL) IS TRUE)
);
CREATE TABLE validation_wiring_capture_records(
 command_id varchar(32) PRIMARY KEY REFERENCES validation_wiring_commands(command_id),
 origin varchar(8) NOT NULL CHECK(origin IN('real','sim')),
 collected_at timestamptz,
 stopped_at timestamptz NOT NULL,
 raw_key text NOT NULL,
 raw_version text NOT NULL CHECK(length(raw_version)>0 AND raw_version<>'null'),
 raw_sha256 char(64) NOT NULL CHECK(raw_sha256 ~ '^[0-9a-f]{64}$'),
 raw_size bigint NOT NULL CHECK(raw_size BETWEEN 1 AND 2097152),
 evaluation jsonb NOT NULL,
 received_at timestamptz NOT NULL,
 CHECK(collected_at IS NULL OR collected_at<=stopped_at),
 CHECK((evaluation->>'version'='keel.wiring/v1' AND evaluation->>'rule_version'='wiring-three-source/v1' AND evaluation->>'step2' IN('PASS','FAIL','BLOCKED') AND evaluation->>'step3' IN('PASS','FAIL','BLOCKED')) IS TRUE),
 CHECK((evaluation->>'step2'='PASS' OR evaluation->>'step3'='BLOCKED') IS TRUE),
 CHECK(origin='real' OR evaluation->>'step2'<>'PASS')
);
CREATE TRIGGER validation_wiring_readback_query_immutable BEFORE UPDATE OR DELETE ON validation_wiring_readback_queries FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_capture_immutable BEFORE UPDATE OR DELETE ON validation_wiring_capture_records FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_wiring_capture_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c validation_wiring_commands; r jsonb; child varchar; live boolean;
BEGIN
 IF TG_TABLE_NAME='validation_wiring_readback_queries' THEN
  SELECT d.request,d.command_id INTO r,child FROM validation_wiring_readback_dispatches d WHERE d.child_command_id=NEW.child_command_id;
  SELECT * INTO c FROM validation_wiring_commands WHERE command_id=child;
  IF r IS NULL OR (NEW.observation->>'project_id'=r->>'project_id' AND NEW.observation->>'execution_id'=r->>'execution_id' AND NEW.observation->>'command_id'=r->>'command_id' AND NEW.observation->>'fencing_token'=r->>'fencing_token' AND NEW.observation->>'scope_hash'=r->>'scope_hash' AND NEW.observation->>'evidence_source'=r->>'evidence_source' AND (NEW.observation->>'finished_at')::timestamptz>=(r->>'not_before')::timestamptz AND (NEW.observation->>'finished_at')::timestamptz<=clock_timestamp()) IS NOT TRUE
   OR NOT EXISTS(SELECT 1 FROM validation_wiring_readback_dispatches d WHERE d.child_command_id=NEW.child_command_id AND NEW.observation->>'request_hash'=d.content_hash)
   OR (NEW.observation->>'state'='completed' AND (NEW.observation->'result' IS NULL OR NEW.observation->'result'='null'::jsonb))
   OR (NEW.observation->>'state' IN('stopped','rejected') AND NEW.observation->'result' IS DISTINCT FROM 'null'::jsonb) THEN
   RAISE EXCEPTION 'terminal query must bind the immutable original collector request';
  END IF;
 ELSE
  SELECT * INTO c FROM validation_wiring_commands WHERE command_id=NEW.command_id;
  IF c.command_id IS NULL OR NEW.origin IS DISTINCT FROM c.command->>'host_origin' OR NEW.stopped_at<c.created_at OR NEW.stopped_at>clock_timestamp()+interval '5 seconds'
   OR NEW.raw_key IS DISTINCT FROM 'validation/wiring/'||c.project_id||'/'||c.asset_id||'/'||c.command_id||'/'||NEW.raw_sha256||'.json'
   OR EXISTS(SELECT 1 FROM validation_wiring_readback_dispatches d WHERE d.command_id=c.command_id AND NOT EXISTS(SELECT 1 FROM validation_wiring_readback_results x WHERE x.child_command_id=d.child_command_id) AND NOT EXISTS(SELECT 1 FROM validation_wiring_readback_queries q WHERE q.child_command_id=d.child_command_id AND q.observation->>'state' IN('completed','stopped','rejected'))) THEN
   RAISE EXCEPTION 'capture requires original host provenance, versioned raw key and terminal collector scope';
  END IF;
 END IF;
 SELECT a.state='running' INTO live FROM exec_task_attempts a WHERE a.id=c.attempt_id AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND ((a.state='running' AND a.lease_until>clock_timestamp()) OR a.state='unknown');
 IF live IS NULL OR NOT keel_wiring_exclusive_scope(c.attempt_id,c.fencing_token,c.command#>>'{expectation,machine_id}',c.command#>'{expectation,ports}',live) THEN RAISE EXCEPTION 'wiring evidence must retain original held or quarantined shared resources'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_readback_query_scope BEFORE INSERT ON validation_wiring_readback_queries FOR EACH ROW EXECUTE FUNCTION keel_wiring_capture_scope();
CREATE TRIGGER validation_wiring_capture_scope BEFORE INSERT ON validation_wiring_capture_records FOR EACH ROW EXECUTE FUNCTION keel_wiring_capture_scope();
