CREATE TABLE topology_issue_retests(
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL REFERENCES topology_issue_bindings(issue_id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 snapshot_id varchar(32) NOT NULL REFERENCES pci_topology_snapshots(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 repair_version bigint NOT NULL CHECK(repair_version>0),
 applied_issue_version bigint NOT NULL CHECK(applied_issue_version>repair_version),
 scope_hash varchar(64) NOT NULL,
 verdict varchar(8) NOT NULL CHECK(verdict IN('PASS','FAIL','BLOCKED')),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(issue_id,snapshot_id),
 UNIQUE(issue_id,repair_version)
);
CREATE INDEX topology_issue_retest_history ON topology_issue_retests(issue_id,created_at DESC,id DESC);
CREATE TABLE topology_issue_escalations(
 request_id varchar(32) PRIMARY KEY REFERENCES topology_issue_retests(id),
 issue_id varchar(32) NOT NULL REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 recipient_ids jsonb NOT NULL CHECK(jsonb_typeof(recipient_ids)='array'),
 failed_verifications integer NOT NULL CHECK(failed_verifications=2),
 created_at timestamptz NOT NULL
);
CREATE TRIGGER topology_issue_retest_immutable BEFORE UPDATE OR DELETE ON topology_issue_retests FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER topology_issue_escalation_immutable BEFORE UPDATE OR DELETE ON topology_issue_escalations FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_topology_issue_retest_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='topology_issue_retests' THEN
  IF NOT EXISTS(SELECT 1 FROM topology_issue_bindings b JOIN issues i ON i.id=b.issue_id JOIN pci_topology_snapshots s ON s.id=NEW.snapshot_id JOIN validation_precollect_requests q ON q.id=s.precollect_id JOIN issue_events repair ON repair.issue_id=i.id AND repair.version=NEW.repair_version AND repair.action='repair' WHERE b.issue_id=NEW.issue_id AND b.project_id=NEW.project_id AND s.project_id=b.project_id AND s.asset_id=b.asset_id AND s.machine_id=b.machine_id AND s.origin=b.origin AND q.execution_id=NEW.execution_id AND q.created_at>repair.created_at AND s.created_at>repair.created_at AND s.scope_hash=NEW.scope_hash AND s.verdict=NEW.verdict AND s.valid_until>statement_timestamp() AND i.version=NEW.applied_issue_version AND ((NEW.verdict='PASS' AND i.state='verify' AND s.origin='real') OR (NEW.verdict<>'PASS' AND i.state='in_repair')) AND repair.version=(SELECT max(e.version) FROM issue_events e WHERE e.issue_id=i.id AND e.action='repair')) THEN RAISE EXCEPTION 'T7 retest requires fresh post-repair scoped topology and original execution'; END IF;
 ELSIF TG_TABLE_NAME='topology_issue_escalations' THEN
  IF NOT EXISTS(SELECT 1 FROM topology_issue_retests r WHERE r.id=NEW.request_id AND r.issue_id=NEW.issue_id AND r.project_id=NEW.project_id AND r.execution_id=NEW.execution_id AND r.verdict='FAIL') THEN RAISE EXCEPTION 'T7 escalation requires a failed original retest'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER topology_issue_retest_scope BEFORE INSERT ON topology_issue_retests FOR EACH ROW EXECUTE FUNCTION keel_topology_issue_retest_scope();
CREATE TRIGGER topology_issue_escalation_scope BEFORE INSERT ON topology_issue_escalations FOR EACH ROW EXECUTE FUNCTION keel_topology_issue_retest_scope();
