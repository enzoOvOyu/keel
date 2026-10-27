-- Match the binary pgx timestamptz projection, while original request JSON
-- retains every nanosecond. PostgreSQL text casts otherwise round fractional
-- digits and can disagree with the binary driver's integer microseconds.
CREATE FUNCTION keel_wiring_original_timestamp(original text) RETURNS timestamptz LANGUAGE sql STABLE STRICT AS $$
 SELECT regexp_replace(original,'(\.[0-9]{6})[0-9]+','\1')::timestamptz
$$;
CREATE OR REPLACE FUNCTION keel_wiring_readback_scope() RETURNS trigger LANGUAGE plpgsql AS $$
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
  IF c.command_id IS NULL OR keel_wiring_original_timestamp(r->>'not_before') IS DISTINCT FROM advertising
   OR EXISTS(SELECT 1 FROM validation_wiring_readback_dispatches original WHERE original.command_id=c.command_id AND original.request->'not_before' IS DISTINCT FROM r->'not_before')
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
