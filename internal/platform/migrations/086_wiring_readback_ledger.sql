-- Source only: the shared Execution/privileged host adapter is not enabled by
-- these tables. Step 2--4 insertion remains blocked by migration 085.
CREATE TABLE validation_wiring_commands(
 command_id varchar(32) PRIMARY KEY REFERENCES exec_task_attempts(command_id),
 attempt_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 batch_id varchar(32) NOT NULL,
 asset_id varchar(32) NOT NULL,
 handshake_id varchar(32) NOT NULL UNIQUE REFERENCES validation_wiring_handshakes(id),
 parent_id varchar(32) NOT NULL REFERENCES validation_wiring_step_reports(id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 origin varchar(20) NOT NULL CHECK(origin IN('device','protocol_fixture')),
 plan_digest char(64) NOT NULL CHECK(plan_digest ~ '^[0-9a-f]{64}$'),
 command jsonb NOT NULL,
 created_at timestamptz NOT NULL,
 FOREIGN KEY(batch_id,asset_id) REFERENCES validation_wiring_batch_targets(batch_id,asset_id),
 CHECK((command->>'batch_id'=batch_id AND command->>'asset_id'=asset_id AND command->>'handshake_id'=handshake_id AND command->>'parent_id'=parent_id AND command#>>'{binding,project_id}'=project_id AND command#>>'{binding,execution_id}'=execution_id AND command#>>'{binding,attempt_id}'=attempt_id AND command#>>'{binding,command_id}'=command_id AND (command#>>'{binding,fencing_token}')::bigint=fencing_token AND command#>>'{binding,origin}'=origin AND command#>>'{plan,digest}'=plan_digest AND command#>>'{plan,command_id}'=command_id) IS TRUE)
);
CREATE TABLE validation_wiring_readback_windows(
 command_id varchar(32) PRIMARY KEY REFERENCES validation_wiring_commands(command_id),
 advertising_at timestamptz NOT NULL
);
CREATE TABLE validation_wiring_readback_dispatches(
 child_command_id varchar(32) PRIMARY KEY CHECK(child_command_id ~ '^[0-9a-f]{32}$'),
 command_id varchar(32) NOT NULL REFERENCES validation_wiring_readback_windows(command_id),
 request jsonb NOT NULL,
 content_hash char(64) NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 committed_at timestamptz NOT NULL,
 CHECK((request->>'command_id'=child_command_id AND request->>'version'='fabric-recollect/v1' AND jsonb_array_length(request->'targets')=1 AND jsonb_array_length(request#>'{targets,0,ports}') BETWEEN 1 AND 2) IS TRUE)
);
CREATE INDEX validation_wiring_readback_original ON validation_wiring_readback_dispatches(command_id,child_command_id);
CREATE TABLE validation_wiring_readback_results(
 child_command_id varchar(32) PRIMARY KEY REFERENCES validation_wiring_readback_dispatches(child_command_id),
 response jsonb NOT NULL,
 content_hash char(64) NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 received_at timestamptz NOT NULL
);
CREATE TRIGGER validation_wiring_command_immutable BEFORE UPDATE OR DELETE ON validation_wiring_commands FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_window_immutable BEFORE UPDATE OR DELETE ON validation_wiring_readback_windows FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_readback_dispatch_immutable BEFORE UPDATE OR DELETE ON validation_wiring_readback_dispatches FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_readback_result_immutable BEFORE UPDATE OR DELETE ON validation_wiring_readback_results FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();

-- Resource ownership remains in the common execution lease table. There is no
-- wiring-local scheduler or second locking system.
CREATE FUNCTION keel_wiring_exclusive_scope(attempt varchar, fence bigint, machine varchar, ports jsonb, live boolean) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT jsonb_array_length(ports)>0 AND NOT EXISTS(
  SELECT 1 FROM (
   SELECT 'device:machine:'||machine AS resource UNION SELECT 'ssh:machine:'||machine
   UNION SELECT 'device:switch:'||(p->>'switch_id') FROM jsonb_array_elements(ports) p
   UNION SELECT 'ssh:switch:'||(p->>'switch_id') FROM jsonb_array_elements(ports) p
  ) selected WHERE NOT EXISTS(
   SELECT 1 FROM exec_resource_leases l WHERE l.attempt_id=attempt AND l.fencing_token=fence AND l.resource_id=selected.resource AND l.mode='exclusive'
   AND ((live AND l.state='held' AND l.expires_at>clock_timestamp()) OR (NOT live AND l.state IN('held','reconciling')))
  )
 )
$$;

-- Serialize BOTH kinds of lease consumption on the original handshake row.
-- A handshake consumed as Step 1 cannot later authorize hardware dispatch.
CREATE FUNCTION keel_wiring_handshake_claim() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM validation_wiring_handshakes WHERE id=NEW.handshake_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM validation_wiring_batches WHERE handshake_id=NEW.handshake_id)
 OR EXISTS(SELECT 1 FROM validation_wiring_commands WHERE handshake_id=NEW.handshake_id) THEN
  RAISE EXCEPTION 'wiring readiness handshake is already consumed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_batch_handshake_claim BEFORE INSERT ON validation_wiring_batches FOR EACH ROW EXECUTE FUNCTION keel_wiring_handshake_claim();
CREATE TRIGGER validation_wiring_command_handshake_claim BEFORE INSERT ON validation_wiring_commands FOR EACH ROW EXECUTE FUNCTION keel_wiring_handshake_claim();

CREATE FUNCTION keel_wiring_command_scope() RETURNS trigger LANGUAGE plpgsql AS $$
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
  AND NEW.command->'expectation'=t.expectation AND NEW.command#>>'{plan,version}'='keel.wiring/v1' AND NEW.command#>>'{plan,scope_hash}'=t.expectation->>'scope_hash'
  AND h.project_id=NEW.project_id AND h.valid_until>clock_timestamp() AND h.collector->>'state'='ready'
  AND jsonb_array_length(h.facts->'targets')=1 AND h.facts#>>'{targets,0,asset_id}'=t.asset_id AND h.facts#>>'{targets,0,topology_id}'=t.topology_id
  AND a.command_id=NEW.command_id AND a.execution_id=NEW.execution_id AND a.fencing_token=NEW.fencing_token AND a.state='running' AND a.lease_until>clock_timestamp()
  AND target.kind='machine' AND target.resource_key=t.machine_id AND x.project_id=t.project_id AND x.mode='fabric' AND x.desired_state='running' AND x.parameters->>'validation_kind'='wiring'
  AND keel_wiring_exclusive_scope(a.id,a.fencing_token,t.machine_id,t.expectation->'ports',true)) THEN
  RAISE EXCEPTION 'LLDP start requires current Step 1, a new readiness lease and complete shared exclusive resources';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_command_scope BEFORE INSERT ON validation_wiring_commands FOR EACH ROW EXECUTE FUNCTION keel_wiring_command_scope();
CREATE FUNCTION keel_wiring_readback_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE c validation_wiring_commands; r jsonb; advertising timestamptz; live boolean; parent_command varchar;
BEGIN
 IF TG_TABLE_NAME='validation_wiring_readback_windows' THEN
  SELECT * INTO c FROM validation_wiring_commands WHERE command_id=NEW.command_id;
  IF c.command_id IS NULL OR NEW.advertising_at<c.created_at OR NEW.advertising_at>clock_timestamp()+interval '5 seconds' THEN RAISE EXCEPTION 'advertising window must follow its registered command'; END IF;
  RETURN NEW;
 ELSIF TG_TABLE_NAME='validation_wiring_readback_dispatches' THEN
  SELECT * INTO c FROM validation_wiring_commands WHERE command_id=NEW.command_id;
  SELECT advertising_at INTO advertising FROM validation_wiring_readback_windows WHERE command_id=NEW.command_id;
  r:=NEW.request;
  IF c.command_id IS NULL OR (r->>'not_before')::timestamptz IS DISTINCT FROM advertising
   OR (r->>'project_id'=c.project_id AND r->>'execution_id'=c.execution_id AND (r->>'fencing_token')::bigint=c.fencing_token AND r->>'scope_hash'=c.command#>>'{expectation,scope_hash}' AND r->>'evidence_source'=c.origin) IS NOT TRUE
   OR NOT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE a.id=c.attempt_id AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND a.state='running' AND a.lease_until>clock_timestamp() AND x.desired_state='running'
    AND keel_wiring_exclusive_scope(a.id,a.fencing_token,c.command#>>'{expectation,machine_id}',c.command#>'{expectation,ports}',true))
   OR EXISTS(SELECT 1 FROM jsonb_array_elements_text(r#>'{targets,0,ports}') AS names(port_name) WHERE NOT EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p->>'switch_id'=r#>>'{targets,0,switch_id}' AND p->>'switch_serial'=r#>>'{targets,0,serial}' AND p->>'switch_port'=port_name)) THEN
   RAISE EXCEPTION 'readback dispatch must retain the original window, selected ports and live shared leases';
  END IF;
 ELSE
  SELECT d.request,d.command_id INTO r,parent_command FROM validation_wiring_readback_dispatches d WHERE d.child_command_id=NEW.child_command_id;
  SELECT * INTO c FROM validation_wiring_commands WHERE command_id=parent_command;
  IF r IS NULL OR (NEW.response->>'version'=r->>'version' AND NEW.response->>'project_id'=r->>'project_id' AND NEW.response->>'execution_id'=r->>'execution_id' AND NEW.response->>'command_id'=r->>'command_id' AND NEW.response->>'fencing_token'=r->>'fencing_token' AND NEW.response->>'scope_hash'=r->>'scope_hash' AND NEW.response->>'evidence_source'=r->>'evidence_source') IS NOT TRUE THEN RAISE EXCEPTION 'readback result must match its immutable original child command'; END IF;
  SELECT a.state='running' INTO live FROM exec_task_attempts a WHERE a.id=c.attempt_id AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND ((a.state='running' AND a.lease_until>clock_timestamp()) OR a.state='unknown');
  IF live IS NULL OR NOT keel_wiring_exclusive_scope(c.attempt_id,c.fencing_token,c.command#>>'{expectation,machine_id}',c.command#>'{expectation,ports}',live) THEN RAISE EXCEPTION 'readback completion requires held or quarantined original shared resources'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_window_scope BEFORE INSERT ON validation_wiring_readback_windows FOR EACH ROW EXECUTE FUNCTION keel_wiring_readback_scope();
CREATE TRIGGER validation_wiring_readback_dispatch_scope BEFORE INSERT ON validation_wiring_readback_dispatches FOR EACH ROW EXECUTE FUNCTION keel_wiring_readback_scope();
CREATE TRIGGER validation_wiring_readback_result_scope BEFORE INSERT ON validation_wiring_readback_results FOR EACH ROW EXECUTE FUNCTION keel_wiring_readback_scope();
