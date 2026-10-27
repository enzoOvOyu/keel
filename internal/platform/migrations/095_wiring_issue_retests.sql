CREATE TABLE validation_wiring_issue_retests(
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL REFERENCES validation_wiring_issue_bindings(issue_id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 report_id varchar(32) NOT NULL REFERENCES validation_wiring_step_reports(id),
 command_id varchar(32) NOT NULL REFERENCES validation_wiring_commands(command_id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 repair_version bigint NOT NULL CHECK(repair_version>0),
 applied_issue_version bigint NOT NULL CHECK(applied_issue_version>repair_version),
 verdict varchar(8) NOT NULL CHECK(verdict IN('PASS','FAIL','BLOCKED')),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(issue_id,repair_version),
 UNIQUE(issue_id,command_id)
);
CREATE TABLE validation_wiring_issue_escalations(
 retest_id varchar(32) PRIMARY KEY REFERENCES validation_wiring_issue_retests(id),
 issue_id varchar(32) NOT NULL REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 recipient_ids jsonb NOT NULL CHECK(jsonb_typeof(recipient_ids)='array'),
 failed_verifications integer NOT NULL CHECK(failed_verifications=2),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER validation_wiring_retest_immutable BEFORE UPDATE OR DELETE ON validation_wiring_issue_retests FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER validation_wiring_escalation_immutable BEFORE UPDATE OR DELETE ON validation_wiring_issue_escalations FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_wiring_retest_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id JOIN issue_events repair ON repair.issue_id=i.id AND repair.version=NEW.repair_version AND repair.action='repair' JOIN validation_wiring_commands c ON c.command_id=NEW.command_id JOIN validation_wiring_step_reports r ON r.id=NEW.report_id JOIN exec_task_attempts a ON a.id=c.attempt_id WHERE b.issue_id=NEW.issue_id AND b.project_id=NEW.project_id AND i.version=NEW.applied_issue_version AND c.project_id=b.project_id AND c.asset_id=b.asset_id AND c.command->>'host_origin'=b.host_origin AND c.origin=b.switch_origin AND c.execution_id=NEW.execution_id AND c.created_at>repair.created_at AND r.detail->>'command_id'=c.command_id AND r.step IN(2,3) AND r.verdict=NEW.verdict AND r.valid_until>clock_timestamp() AND a.state IN('success','failed') AND a.command_id=c.command_id AND a.fencing_token=c.fencing_token AND (NEW.verdict<>'PASS' OR r.step=3) AND EXISTS(SELECT 1 FROM jsonb_array_elements(c.command#>'{expectation,ports}') p WHERE p->>'physical_port_id'=b.physical_port_id) AND NOT EXISTS(SELECT 1 FROM issue_events newer WHERE newer.issue_id=i.id AND newer.version>repair.version AND newer.action IN('repair','assign','reopen'))) THEN RAISE EXCEPTION 'wiring retest requires a new settled original capture after the current repair'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_retest_scope BEFORE INSERT ON validation_wiring_issue_retests FOR EACH ROW EXECUTE FUNCTION keel_wiring_retest_scope();
