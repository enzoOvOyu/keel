-- Port monitoring uses the shared issue Execution, original dispatch/query,
-- stopped evidence, result and escalation ledgers. It never rewrites source=g2.
CREATE OR REPLACE FUNCTION keel_issue_verification_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='issue_verification_requests' THEN
  IF NOT EXISTS(SELECT 1 FROM issues i JOIN exec_executions e ON e.id=NEW.execution_id
   WHERE i.id=NEW.issue_id AND i.project_id=NEW.project_id AND e.project_id=i.project_id
   AND ((i.source='fabric' AND i.category IN('T1','T2')) OR (i.source='portmon' AND i.category IN('T1','T3','T4','T6') AND NEW.snapshot->'port_monitor' IS NOT NULL)) AND i.state='verify' AND i.version=NEW.issue_version+1
   AND e.mode='fabric' AND e.parameters->>'issue_verification_id'=NEW.id
   AND e.parameters->>'issue_id'=i.id AND e.created_by=NEW.created_by
   AND e.snapshot#>>'{flow,definition,api_version}'='keel.issue-verify/v1')
   OR NEW.snapshot->>'scope_hash' IS DISTINCT FROM NEW.scope_hash
   OR NEW.snapshot->>'issue_id' IS DISTINCT FROM NEW.issue_id
   OR NEW.snapshot->>'project_id' IS DISTINCT FROM NEW.project_id
   OR (NEW.snapshot->>'issue_version')::bigint IS DISTINCT FROM NEW.issue_version THEN
   RAISE EXCEPTION 'Verification request does not match the admitted issue execution';
  END IF;
 ELSIF TG_TABLE_NAME='issue_verification_evidence' THEN
  IF NOT EXISTS(SELECT 1 FROM issue_verification_requests q JOIN exec_task_attempts a ON a.execution_id=q.execution_id
   WHERE q.id=NEW.request_id AND a.command_id=NEW.command_id AND a.fencing_token=NEW.fencing_token
   AND a.state='running' AND a.lease_until>now()
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(q.snapshot->'targets') t
    WHERE NOT EXISTS(SELECT 1 FROM exec_resource_leases l WHERE l.attempt_id=a.id
      AND l.resource_id='ssh:switch:'||(t->>'switch_id') AND l.fencing_token=a.fencing_token
      AND l.mode='exclusive' AND l.state='held' AND l.expires_at>now()))
   AND NEW.request->>'execution_id'=q.execution_id
   AND NEW.request->>'command_id'=a.command_id AND NEW.request->>'scope_hash'=q.scope_hash
   AND NEW.request->>'project_id'=q.project_id AND (NEW.request->>'fencing_token')::bigint=a.fencing_token) THEN
   RAISE EXCEPTION 'Verification evidence requires the current scoped execution fence';
  END IF;
 ELSIF TG_TABLE_NAME='issue_escalations' THEN
  IF NOT EXISTS(SELECT 1 FROM issue_verification_requests q JOIN issue_verification_results r ON r.request_id=q.id
   WHERE q.id=NEW.request_id AND q.issue_id=NEW.issue_id AND q.project_id=NEW.project_id
   AND q.execution_id=NEW.execution_id AND r.verdict='FAIL') THEN
   RAISE EXCEPTION 'Escalation requires a matching failed verification';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM issue_verification_evidence e WHERE e.id=NEW.evidence_id AND e.request_id=NEW.request_id) THEN
   RAISE EXCEPTION 'Verification result requires matching immutable evidence';
  END IF;
 END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION keel_portmon_verification_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE frozen jsonb; request_body jsonb; execution_ref text; issue_ref text; original_issue bigint; wanted_state text; attempt_ref text; fence bigint;
BEGIN
 IF TG_TABLE_NAME='issue_verification_requests' THEN
  frozen:=NEW.snapshot;
  IF frozen->'port_monitor' IS NULL THEN RETURN NEW; END IF;
  IF NOT EXISTS(SELECT 1 FROM portmon_issue_bindings discovery JOIN issues issue ON issue.id=discovery.issue_id JOIN project_assets asset ON asset.id=discovery.asset_id JOIN monitor_metric_samples original ON original.sequence=discovery.sample_sequence JOIN mapping_revisions revision ON revision.asset_id=asset.id AND revision.id=frozen#>>'{port_monitor,mapping_revision_id}' JOIN mapping_assessments assessment ON assessment.id=revision.assessment_id CROSS JOIN LATERAL jsonb_array_elements(assessment.candidate->'bindings') binding JOIN issue_events repair ON repair.issue_id=issue.id AND repair.action='repair' AND repair.version=(frozen->>'repair_version')::bigint
   WHERE issue.id=NEW.issue_id AND issue.project_id=NEW.project_id AND issue.source='portmon' AND asset.project_id=NEW.project_id AND asset.id=frozen#>>'{port_monitor,asset_id}' AND asset.version=(frozen#>>'{port_monitor,asset_version}')::bigint AND binding#>>'{plan,physical_port_id}'=discovery.physical_port_id AND binding#>>'{plan,role}'<>'unused' AND binding#>>'{observed,interface}'=frozen#>>'{port_monitor,diagnostic,interface}' AND binding#>>'{observed,permanent_mac}'=frozen#>>'{port_monitor,diagnostic,permanent_mac}' AND binding#>>'{observed,bdf}'=frozen#>>'{port_monitor,diagnostic,bdf}' AND binding#>>'{observed,switch_id}'=frozen#>>'{port_monitor,diagnostic,switch_id}' AND binding#>>'{observed,switch_port}'=frozen#>>'{port_monitor,diagnostic,switch_port}' AND frozen#>>'{port_monitor,diagnostic,physical_port_id}'=discovery.physical_port_id AND frozen#>>'{port_monitor,diagnostic,rule_id}'=discovery.rule_id AND frozen#>>'{port_monitor,diagnostic,category}'=issue.category AND frozen#>>'{port_monitor,diagnostic,project_id}'=NEW.project_id AND frozen#>>'{port_monitor,diagnostic,issue_id}'=issue.id AND frozen#>>'{port_monitor,diagnostic,machine_id}'=asset.machine_id AND frozen#>>'{port_monitor,diagnostic,source}'=original.source AND frozen#>>'{port_monitor,diagnostic,method}'=original.method AND frozen#>>'{port_monitor,diagnostic,origin}'=original.origin AND keel_wiring_original_timestamp(frozen#>>'{port_monitor,diagnostic,repaired_at}')=repair.created_at AND NOT EXISTS(SELECT 1 FROM issue_verification_requests prior WHERE prior.issue_id=issue.id AND prior.snapshot->>'repair_version'=frozen->>'repair_version')) THEN RAISE EXCEPTION 'port verification requires its immutable discovery and one exact current repair'; END IF;
  RETURN NEW;
 END IF;
 SELECT snapshot,execution_id INTO frozen,execution_ref FROM issue_verification_requests WHERE id=NEW.request_id;
 IF frozen->'port_monitor' IS NULL THEN RETURN NEW; END IF;
 IF TG_TABLE_NAME='issue_verification_queries' THEN
  wanted_state:='reconciling';
 ELSE
  request_body:=NEW.request;
  wanted_state:='held';
  IF TG_TABLE_NAME='issue_verification_evidence' THEN
   IF NEW.query_id IS NOT NULL THEN wanted_state:='reconciling'; END IF;
  END IF;
  IF request_body->'port_diagnostic' IS DISTINCT FROM frozen#>'{port_monitor,diagnostic}' OR request_body->>'version'<>'keel.port-diagnostic/v1' THEN RAISE EXCEPTION 'port diagnostic dispatch must preserve frozen scope'; END IF;
 END IF;
 SELECT id,fencing_token INTO attempt_ref,fence FROM exec_task_attempts WHERE execution_id=execution_ref AND command_id=NEW.command_id AND fencing_token=NEW.fencing_token;
 IF request_body IS NOT NULL AND request_body->>'attempt_id' IS DISTINCT FROM attempt_ref THEN RAISE EXCEPTION 'port diagnostic attempt must match the original command'; END IF;
 IF attempt_ref IS NULL OR EXISTS(SELECT 1 FROM unnest(ARRAY['device:machine:','ssh:machine:','agent:machine:']) prefix WHERE NOT EXISTS(SELECT 1 FROM exec_resource_leases lease WHERE lease.attempt_id=attempt_ref AND lease.fencing_token=fence AND lease.resource_id=prefix||(frozen#>>'{port_monitor,diagnostic,machine_id}') AND lease.mode='exclusive' AND lease.state=wanted_state AND (wanted_state='reconciling' OR lease.expires_at>clock_timestamp()))) THEN RAISE EXCEPTION 'port diagnostic requires its complete original host resource scope'; END IF;
 IF NOT EXISTS(SELECT 1 FROM exec_resource_leases lease WHERE lease.attempt_id=attempt_ref AND lease.fencing_token=fence AND lease.resource_id='device:switch:'||(frozen#>>'{port_monitor,diagnostic,switch_id}') AND lease.mode='exclusive' AND lease.state=wanted_state AND (wanted_state='reconciling' OR lease.expires_at>clock_timestamp())) THEN RAISE EXCEPTION 'port diagnostic requires its peer device resource'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER portmon_verification_request_scope BEFORE INSERT ON issue_verification_requests FOR EACH ROW EXECUTE FUNCTION keel_portmon_verification_scope();
CREATE TRIGGER portmon_verification_dispatch_scope BEFORE INSERT ON issue_verification_dispatches FOR EACH ROW EXECUTE FUNCTION keel_portmon_verification_scope();
CREATE TRIGGER portmon_verification_evidence_scope BEFORE INSERT ON issue_verification_evidence FOR EACH ROW EXECUTE FUNCTION keel_portmon_verification_scope();
CREATE TRIGGER portmon_verification_query_scope BEFORE INSERT ON issue_verification_queries FOR EACH ROW EXECUTE FUNCTION keel_portmon_verification_scope();

ALTER TABLE issue_verification_results ADD COLUMN port_checks jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(port_checks)='array');
