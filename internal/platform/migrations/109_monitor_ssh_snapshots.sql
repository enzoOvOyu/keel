-- Agentless work shares Execution, SSH identity, leases and original receipts.
CREATE TABLE monitor_ssh_requests(
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 asset_version bigint NOT NULL,
 network_only boolean NOT NULL,
 policy_id varchar(64) NOT NULL,
 payload_bytes_per_second integer NOT NULL CHECK(payload_bytes_per_second BETWEEN 1024 AND 50000),
 interval_seconds integer NOT NULL CHECK(interval_seconds BETWEEN 300 AND 900),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(execution_id,machine_id)
);
CREATE TABLE monitor_ssh_captures(
 command_id varchar(32) PRIMARY KEY REFERENCES exec_task_attempts(command_id),
 attempt_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(id),
 execution_id varchar(32) NOT NULL,
 machine_id varchar(32) NOT NULL,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 command jsonb NOT NULL,
 raw_key text NOT NULL,
 raw_version text NOT NULL CHECK(raw_version<>'' AND raw_version<>'null'),
 raw_sha256 varchar(64) NOT NULL,
 raw_size bigint NOT NULL CHECK(raw_size BETWEEN 1 AND 524288),
 snapshot jsonb NOT NULL,
 collected_at timestamptz NOT NULL,
 received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(execution_id,machine_id) REFERENCES monitor_ssh_requests(execution_id,machine_id)
);
CREATE TRIGGER monitor_ssh_request_immutable BEFORE UPDATE OR DELETE ON monitor_ssh_requests FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER monitor_ssh_capture_immutable BEFORE UPDATE OR DELETE ON monitor_ssh_captures FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_monitor_ssh_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='monitor_ssh_requests' THEN
  IF NOT EXISTS(SELECT 1 FROM project_assets asset JOIN exec_executions execution ON execution.id=NEW.execution_id JOIN exec_execution_targets target ON target.execution_id=execution.id AND target.snapshot->>'resource_key'=NEW.machine_id WHERE asset.id=NEW.asset_id AND asset.project_id=NEW.project_id AND asset.machine_id=NEW.machine_id AND asset.version=NEW.asset_version AND execution.project_id=NEW.project_id AND execution.mode='ssh' AND execution.parameters->>'monitor_kind'='snapshot' AND (execution.parameters->>'monitor_network_only')::boolean=NEW.network_only AND target.snapshot->>'monitor'='true' AND (target.snapshot->>'revision')::bigint=NEW.asset_version) THEN RAISE EXCEPTION 'monitor SSH request requires the frozen project asset and domain execution'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM monitor_ssh_requests request JOIN exec_task_attempts attempt ON attempt.execution_id=request.execution_id JOIN exec_targets target ON target.id=attempt.target_id WHERE request.execution_id=NEW.execution_id AND request.machine_id=NEW.machine_id AND request.project_id=NEW.project_id AND attempt.id=NEW.attempt_id AND attempt.command_id=NEW.command_id AND attempt.fencing_token=NEW.fencing_token AND target.resource_key=NEW.machine_id AND NEW.command->>'operation'='host.monitor' AND NEW.command->>'project_id'=NEW.project_id AND NEW.command->>'machine_id'=NEW.machine_id AND NEW.command->>'command_id'=NEW.command_id AND NEW.command->>'attempt_id'=NEW.attempt_id AND (NEW.command->>'fencing_token')::bigint=NEW.fencing_token AND coalesce((NEW.command->>'monitor_network_only')::boolean,false)=request.network_only AND (NEW.snapshot->>'network_only')::boolean=request.network_only AND NEW.snapshot->>'version'='keel.monitor-snapshot/v1' AND NEW.collected_at=keel_wiring_original_timestamp(NEW.snapshot->>'collected_at') AND EXISTS(SELECT 1 FROM exec_outbox event WHERE event.execution_id=request.execution_id AND event.event_type='monitor.dispatch_committed' AND event.payload->'command'=NEW.command AND keel_wiring_original_timestamp(NEW.snapshot->>'started_at')>=keel_wiring_original_timestamp(event.payload->>'authorized_at') AND NEW.collected_at<=to_timestamp((NEW.command->>'authorization_expires_at')::numeric/1000000))) THEN RAISE EXCEPTION 'monitor SSH capture requires the exact original bounded dispatched command'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER monitor_ssh_request_scope BEFORE INSERT ON monitor_ssh_requests FOR EACH ROW EXECUTE FUNCTION keel_monitor_ssh_scope();
CREATE TRIGGER monitor_ssh_capture_scope BEFORE INSERT ON monitor_ssh_captures FOR EACH ROW EXECUTE FUNCTION keel_monitor_ssh_scope();
CREATE TABLE monitor_ssh_sample_receipts(
 frame_id varchar(64) PRIMARY KEY,
 command_id varchar(32) NOT NULL REFERENCES monitor_ssh_captures(command_id),
 sample_index integer NOT NULL CHECK(sample_index BETWEEN 0 AND 2047),
 received_at timestamptz NOT NULL,
 UNIQUE(command_id,sample_index)
);
CREATE TRIGGER monitor_ssh_sample_receipt_immutable BEFORE UPDATE OR DELETE ON monitor_ssh_sample_receipts FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_monitor_ssh_sample_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.source='host' AND NEW.method='ssh' THEN
  IF NOT EXISTS(SELECT 1 FROM monitor_ssh_sample_receipts receipt JOIN monitor_ssh_captures capture ON capture.command_id=receipt.command_id JOIN monitor_ssh_requests request ON request.execution_id=capture.execution_id AND request.machine_id=capture.machine_id JOIN exec_runner_receipts settled ON settled.command_id=capture.command_id AND settled.attempt_id=capture.attempt_id AND settled.fencing_token=capture.fencing_token WHERE receipt.frame_id=NEW.frame_id AND capture.project_id=NEW.project_id AND capture.machine_id=NEW.machine_id AND settled.result='success' AND settled.payload#>>'{output,evidence_hash}'=capture.raw_sha256 AND NEW.source_ref='ssh:'||capture.command_id AND NEW.source_hash=capture.raw_sha256 AND NEW.snapshot_only AND NEW.source_valid_until=capture.collected_at+make_interval(secs=>request.interval_seconds) AND NEW.credential_id IS NULL AND NEW.origin='real' AND NEW.sample=capture.snapshot->'samples'->receipt.sample_index AND NEW.observed_at=keel_wiring_original_timestamp(NEW.sample->>'observed_at') AND NEW.entity_key=NEW.sample->>'entity' AND NEW.metric_key=NEW.sample->>'key' AND NEW.policy_id=request.policy_id AND NEW.interval_seconds=request.interval_seconds) THEN RAISE EXCEPTION 'SSH monitoring must retain the original settled command sample, policy, time and validity'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER monitor_ssh_sample_scope BEFORE INSERT ON monitor_metric_samples FOR EACH ROW EXECUTE FUNCTION keel_monitor_ssh_sample_scope();
