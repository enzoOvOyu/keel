-- Persist the exact first dispatch before any remote POST. Recovery only queries
-- this immutable command; a missing HTTP result never authorizes a new run.
CREATE TABLE issue_verification_dispatches (
 request_id varchar(32) PRIMARY KEY REFERENCES issue_verification_requests(id),
 command_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(command_id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 request jsonb NOT NULL,
 content_hash char(64) NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 committed_at timestamptz NOT NULL
);
CREATE TABLE issue_verification_queries (
 id varchar(32) PRIMARY KEY,
 request_id varchar(32) NOT NULL UNIQUE REFERENCES issue_verification_requests(id),
 command_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(command_id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 observation jsonb NOT NULL,
 content_hash char(64) NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 observed_at timestamptz NOT NULL
);
ALTER TABLE issue_verification_evidence ADD COLUMN query_id varchar(32) REFERENCES issue_verification_queries(id);
CREATE FUNCTION keel_issue_verification_dispatch_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='issue_verification_dispatches' THEN
  IF NOT EXISTS(SELECT 1 FROM issue_verification_requests q JOIN exec_task_attempts a ON a.execution_id=q.execution_id JOIN exec_executions x ON x.id=a.execution_id
   WHERE q.id=NEW.request_id AND a.command_id=NEW.command_id AND a.fencing_token=NEW.fencing_token
   AND a.state='running' AND a.lease_until>now() AND x.desired_state='running'
   AND NEW.request->>'project_id'=q.project_id AND NEW.request->>'execution_id'=q.execution_id
   AND NEW.request->>'command_id'=a.command_id AND (NEW.request->>'fencing_token')::bigint=a.fencing_token
   AND NEW.request->>'scope_hash'=q.scope_hash AND NEW.request->>'evidence_source'=q.snapshot->>'evidence_origin'
   AND NEW.request->'targets'=q.snapshot->'targets'
   AND jsonb_array_length(q.snapshot->'targets')>0
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(q.snapshot->'targets') t WHERE NOT EXISTS(
    SELECT 1 FROM exec_resource_leases l WHERE l.attempt_id=a.id AND l.fencing_token=a.fencing_token
    AND l.resource_id='ssh:switch:'||(t->>'switch_id') AND l.state='held' AND l.mode='exclusive' AND l.expires_at>now()))) THEN
   RAISE EXCEPTION 'Recollection dispatch requires the current complete exclusive scope';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM issue_verification_requests q JOIN exec_task_attempts a ON a.execution_id=q.execution_id
   WHERE q.id=NEW.request_id AND a.command_id=NEW.command_id AND a.fencing_token=NEW.fencing_token AND a.state='unknown'
   AND NEW.observation->>'project_id'=q.project_id AND NEW.observation->>'execution_id'=q.execution_id
   AND NEW.observation->>'command_id'=a.command_id AND (NEW.observation->>'fencing_token')::bigint=a.fencing_token
   AND NEW.observation->>'scope_hash'=q.scope_hash AND NEW.observation->>'evidence_source'=q.snapshot->>'evidence_origin'
   AND ((NEW.observation->>'state' IN('completed','stopped','rejected') AND EXISTS(SELECT 1 FROM issue_verification_dispatches d
    WHERE d.request_id=q.id AND d.command_id=a.command_id AND d.fencing_token=a.fencing_token AND NEW.observation->>'request_hash'=d.content_hash))
    OR (NEW.observation->>'state'='local_not_dispatched' AND NEW.observation->'result'='null'::jsonb AND NOT EXISTS(SELECT 1 FROM issue_verification_dispatches d WHERE d.request_id=q.id)))
   AND jsonb_array_length(q.snapshot->'targets')>0
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(q.snapshot->'targets') t WHERE NOT EXISTS(
    SELECT 1 FROM exec_resource_leases l WHERE l.attempt_id=a.id AND l.fencing_token=a.fencing_token
    AND l.resource_id='ssh:switch:'||(t->>'switch_id') AND l.state='reconciling' AND l.mode='exclusive'))) THEN
   RAISE EXCEPTION 'Collector terminal query must retain the original quarantined scope';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER check_issue_verification_dispatch BEFORE INSERT ON issue_verification_dispatches FOR EACH ROW EXECUTE FUNCTION keel_issue_verification_dispatch_scope();
CREATE TRIGGER check_issue_verification_query BEFORE INSERT ON issue_verification_queries FOR EACH ROW EXECUTE FUNCTION keel_issue_verification_dispatch_scope();
CREATE TRIGGER immutable_issue_verification_dispatch BEFORE UPDATE OR DELETE ON issue_verification_dispatches FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER immutable_issue_verification_query BEFORE UPDATE OR DELETE ON issue_verification_queries FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_issue_verification_evidence_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM issue_verification_requests q JOIN exec_task_attempts a ON a.execution_id=q.execution_id
  WHERE q.id=NEW.request_id AND a.command_id=NEW.command_id AND a.fencing_token=NEW.fencing_token
  AND NEW.request->>'execution_id'=q.execution_id AND NEW.request->>'command_id'=a.command_id
  AND NEW.request->>'scope_hash'=q.scope_hash AND NEW.request->>'project_id'=q.project_id
  AND (NEW.request->>'fencing_token')::bigint=a.fencing_token
  AND NEW.request->>'evidence_source'=q.snapshot->>'evidence_origin' AND NEW.request->'targets'=q.snapshot->'targets'
  AND jsonb_array_length(q.snapshot->'targets')>0
  AND ((NEW.query_id IS NULL AND a.state='running' AND a.lease_until>now()
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(q.snapshot->'targets') t WHERE NOT EXISTS(
    SELECT 1 FROM exec_resource_leases l WHERE l.attempt_id=a.id AND l.fencing_token=a.fencing_token
    AND l.resource_id='ssh:switch:'||(t->>'switch_id') AND l.mode='exclusive' AND l.state='held' AND l.expires_at>now()))
   AND ((NEW.response IS NOT NULL AND NEW.response<>'null'::jsonb AND EXISTS(SELECT 1 FROM issue_verification_dispatches d WHERE d.request_id=q.id AND d.command_id=a.command_id AND d.fencing_token=a.fencing_token AND d.request=NEW.request))
    OR (NEW.response='null'::jsonb AND NEW.code='RECOLLECTION_PRESTART_REJECTED')))
  OR (a.state='unknown' AND EXISTS(SELECT 1 FROM issue_verification_queries p
   WHERE p.id=NEW.query_id AND p.request_id=q.id AND p.command_id=a.command_id AND p.fencing_token=a.fencing_token
   AND p.observation->'result' IS NOT DISTINCT FROM NEW.response
   AND (EXISTS(SELECT 1 FROM issue_verification_dispatches d WHERE d.request_id=q.id AND d.command_id=a.command_id AND d.fencing_token=a.fencing_token AND d.request=NEW.request)
    OR (p.observation->>'state'='local_not_dispatched' AND NEW.response='null'::jsonb AND (NEW.request->>'not_before')::timestamptz=q.created_at AND NOT EXISTS(SELECT 1 FROM issue_verification_dispatches d WHERE d.request_id=q.id))))
   AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(q.snapshot->'targets') t WHERE NOT EXISTS(
    SELECT 1 FROM exec_resource_leases l WHERE l.attempt_id=a.id AND l.fencing_token=a.fencing_token
    AND l.resource_id='ssh:switch:'||(t->>'switch_id') AND l.mode='exclusive' AND l.state='reconciling'))))) THEN
  RAISE EXCEPTION 'Verification evidence requires a live scope or authenticated stopped recovery proof';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER check_issue_verification_evidence ON issue_verification_evidence;
CREATE TRIGGER check_issue_verification_evidence BEFORE INSERT ON issue_verification_evidence FOR EACH ROW EXECUTE FUNCTION keel_issue_verification_evidence_scope();
