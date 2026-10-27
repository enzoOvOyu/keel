-- Scoped N-S5 revalidation uses the existing execution kernel and immutable proof.
CREATE TABLE issue_verification_requests (
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 execution_id varchar(32) NOT NULL UNIQUE REFERENCES exec_executions(id),
 issue_version bigint NOT NULL CHECK(issue_version>0),
 scope_hash char(64) NOT NULL CHECK(scope_hash ~ '^[0-9a-f]{64}$'),
 snapshot jsonb NOT NULL,
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 2000),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL
);
CREATE INDEX issue_verification_history ON issue_verification_requests(issue_id,created_at DESC,id DESC);
CREATE TABLE issue_verification_evidence (
 id varchar(32) PRIMARY KEY,
 request_id varchar(32) NOT NULL UNIQUE REFERENCES issue_verification_requests(id),
 command_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(command_id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 request jsonb NOT NULL,
 response jsonb,
 content_hash char(64) NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 code varchar(80) NOT NULL,
 recorded_at timestamptz NOT NULL
);
CREATE TABLE issue_verification_results (
 request_id varchar(32) PRIMARY KEY REFERENCES issue_verification_requests(id),
 evidence_id varchar(32) NOT NULL UNIQUE REFERENCES issue_verification_evidence(id),
 verdict varchar(8) NOT NULL CHECK(verdict IN('PASS','FAIL','BLOCKED')),
 finding jsonb,
 code varchar(80) NOT NULL,
 valid_until timestamptz NOT NULL,
 applied_issue_version bigint,
 evaluated_at timestamptz NOT NULL
);
CREATE FUNCTION keel_issue_verification_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='issue_verification_requests' THEN
  IF NOT EXISTS(SELECT 1 FROM issues i JOIN exec_executions e ON e.id=NEW.execution_id
   WHERE i.id=NEW.issue_id AND i.project_id=NEW.project_id AND e.project_id=i.project_id
   AND i.source='fabric' AND i.category IN('T1','T2') AND i.state='verify' AND i.version=NEW.issue_version+1
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
CREATE TRIGGER check_issue_verification_request BEFORE INSERT ON issue_verification_requests FOR EACH ROW EXECUTE FUNCTION keel_issue_verification_scope();
CREATE TRIGGER check_issue_verification_evidence BEFORE INSERT ON issue_verification_evidence FOR EACH ROW EXECUTE FUNCTION keel_issue_verification_scope();
CREATE TRIGGER check_issue_verification_result BEFORE INSERT ON issue_verification_results FOR EACH ROW EXECUTE FUNCTION keel_issue_verification_scope();
CREATE TRIGGER immutable_issue_verification_request BEFORE UPDATE OR DELETE ON issue_verification_requests FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER immutable_issue_verification_evidence BEFORE UPDATE OR DELETE ON issue_verification_evidence FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER immutable_issue_verification_result BEFORE UPDATE OR DELETE ON issue_verification_results FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TABLE issue_escalations (
 request_id varchar(32) PRIMARY KEY REFERENCES issue_verification_requests(id),
 issue_id varchar(32) NOT NULL REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 recipient_ids jsonb NOT NULL CHECK(jsonb_typeof(recipient_ids)='array'),
 failed_verifications integer NOT NULL CHECK(failed_verifications=2),
 created_at timestamptz NOT NULL
);
CREATE INDEX issue_escalation_history ON issue_escalations(issue_id,created_at DESC);
CREATE TRIGGER check_issue_escalation BEFORE INSERT ON issue_escalations FOR EACH ROW EXECUTE FUNCTION keel_issue_verification_scope();
CREATE TRIGGER immutable_issue_escalation BEFORE UPDATE OR DELETE ON issue_escalations FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
ALTER TABLE notification_webhook_policies DROP CONSTRAINT notification_webhook_policies_event_types_check;
ALTER TABLE notification_webhook_policies ADD CONSTRAINT notification_webhook_policies_event_types_check CHECK(jsonb_typeof(event_types)='array' AND jsonb_array_length(event_types) BETWEEN 1 AND 4);
