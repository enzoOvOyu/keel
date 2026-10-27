-- P2 approval and recovery semantics, with explicit simulated receipts only.
CREATE TABLE topology_firmware_mock_plans(
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL REFERENCES topology_issue_bindings(issue_id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 snapshot jsonb NOT NULL,
 scope_hash char(64) NOT NULL,
 created_at timestamptz NOT NULL,
 valid_until timestamptz NOT NULL CHECK(valid_until>created_at AND valid_until<=created_at+interval '30 minutes'),
 CHECK(snapshot->>'origin'='sim' AND snapshot->>'id'=id AND snapshot->>'issue_id'=issue_id AND snapshot->>'project_id'=project_id AND snapshot->>'scope_hash'=scope_hash AND snapshot->>'created_by'=created_by)
);
CREATE TABLE topology_firmware_mock_approvals(
 plan_id varchar(32) PRIMARY KEY REFERENCES topology_firmware_mock_plans(id),
 scope_hash char(64) NOT NULL,
 approved_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 2000),
 approved_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE topology_firmware_mock_runs(
 plan_id varchar(32) PRIMARY KEY REFERENCES topology_firmware_mock_plans(id),
 execution_id varchar(32) NOT NULL UNIQUE REFERENCES exec_executions(id)
);
CREATE TABLE topology_firmware_mock_receipts(
 command_id varchar(32) PRIMARY KEY REFERENCES exec_task_attempts(command_id),
 attempt_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 plan_id varchar(32) NOT NULL UNIQUE REFERENCES topology_firmware_mock_runs(plan_id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 output jsonb NOT NULL,
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(output->>'origin'='sim' AND output->>'hardware_evidence'='false' AND output->>'mode'='sim')
);
CREATE FUNCTION keel_topology_firmware_mock_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE plan topology_firmware_mock_plans; execution_ref text;
BEGIN
 IF TG_TABLE_NAME='topology_firmware_mock_plans' THEN
  IF NOT EXISTS(SELECT 1 FROM issues issue JOIN project_assets asset ON asset.id=NEW.snapshot->>'asset_id' JOIN device_model_versions model ON model.id=asset.model_version_id JOIN media_items media ON media.id=NEW.snapshot#>>'{artifact,media_id}' JOIN media_artifacts artifact ON artifact.media_id=media.id WHERE issue.id=NEW.issue_id AND issue.project_id=NEW.project_id AND issue.category='T7' AND issue.source='topology' AND issue.state='in_repair' AND issue.version=(NEW.snapshot->>'issue_version')::bigint AND asset.project_id=issue.project_id AND asset.version=(NEW.snapshot->>'asset_version')::bigint AND asset.machine_id=NEW.snapshot->>'machine_id' AND model.id=NEW.snapshot->>'model_version_id' AND model.content_hash=NEW.snapshot->>'model_hash' AND media.type='firmware' AND media.state='approved' AND media.sha256=NEW.snapshot#>>'{artifact,sha256}' AND artifact.object_version=NEW.snapshot#>>'{artifact,object_version}' AND NEW.snapshot->>'reboot_approved'='true') THEN RAISE EXCEPTION 'firmware simulation requires the exact independent model and approved stored artifact'; END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO plan FROM topology_firmware_mock_plans WHERE id=NEW.plan_id;
 IF TG_TABLE_NAME='topology_firmware_mock_approvals' THEN
  IF NEW.approved_by=plan.created_by OR NEW.scope_hash<>plan.scope_hash OR plan.valid_until<=clock_timestamp() OR NOT EXISTS(SELECT 1 FROM users person WHERE person.id=NEW.approved_by AND person.enabled AND (person.is_admin OR EXISTS(SELECT 1 FROM memberships membership WHERE membership.user_id=person.id AND membership.project_id=plan.project_id AND membership.role IN('Admin','DL')))) THEN RAISE EXCEPTION 'independent current DL approval of the exact firmware plan required'; END IF;
 ELSIF TG_TABLE_NAME='topology_firmware_mock_runs' THEN
  IF NOT EXISTS(SELECT 1 FROM exec_executions execution JOIN topology_firmware_mock_approvals approval ON approval.plan_id=plan.id WHERE execution.id=NEW.execution_id AND execution.project_id=plan.project_id AND execution.mode='fabric' AND execution.parameters->>'topology_firmware_plan_id'=plan.id AND execution.snapshot#>>'{flow,definition,api_version}'='keel.topology-firmware-mock/v1' AND plan.valid_until>clock_timestamp()) THEN RAISE EXCEPTION 'firmware mock must use its approved scoped execution'; END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM exec_task_attempts attempt JOIN topology_firmware_mock_runs run ON run.execution_id=attempt.execution_id WHERE run.plan_id=plan.id AND attempt.id=NEW.attempt_id AND attempt.command_id=NEW.command_id AND attempt.fencing_token=NEW.fencing_token AND attempt.execution_id=NEW.execution_id AND attempt.state='running' AND attempt.lease_until>clock_timestamp() AND NEW.output->>'plan_id'=plan.id AND NEW.output->>'scope_hash'=plan.scope_hash AND NEW.output->>'artifact_sha256'=plan.snapshot#>>'{artifact,sha256}' AND NEW.output->>'mock_readback_version'=plan.snapshot#>>'{artifact,version}' AND NOT EXISTS(SELECT 1 FROM unnest(ARRAY['agent:machine:','device:machine:','ssh:machine:']) prefix WHERE NOT EXISTS(SELECT 1 FROM exec_resource_leases lease WHERE lease.attempt_id=attempt.id AND lease.fencing_token=attempt.fencing_token AND lease.resource_id=prefix||(plan.snapshot->>'machine_id') AND lease.state='held' AND lease.mode='exclusive' AND lease.expires_at>clock_timestamp()))) THEN RAISE EXCEPTION 'firmware mock receipt requires its original fenced exclusive scope'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER topology_firmware_plan_scope BEFORE INSERT ON topology_firmware_mock_plans FOR EACH ROW EXECUTE FUNCTION keel_topology_firmware_mock_scope();
CREATE TRIGGER topology_firmware_approval_scope BEFORE INSERT ON topology_firmware_mock_approvals FOR EACH ROW EXECUTE FUNCTION keel_topology_firmware_mock_scope();
CREATE TRIGGER topology_firmware_run_scope BEFORE INSERT ON topology_firmware_mock_runs FOR EACH ROW EXECUTE FUNCTION keel_topology_firmware_mock_scope();
CREATE TRIGGER topology_firmware_receipt_scope BEFORE INSERT ON topology_firmware_mock_receipts FOR EACH ROW EXECUTE FUNCTION keel_topology_firmware_mock_scope();
CREATE TRIGGER topology_firmware_plan_immutable BEFORE UPDATE OR DELETE ON topology_firmware_mock_plans FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER topology_firmware_approval_immutable BEFORE UPDATE OR DELETE ON topology_firmware_mock_approvals FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER topology_firmware_run_immutable BEFORE UPDATE OR DELETE ON topology_firmware_mock_runs FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER topology_firmware_receipt_immutable BEFORE UPDATE OR DELETE ON topology_firmware_mock_receipts FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
