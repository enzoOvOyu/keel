ALTER TABLE exec_targets DROP CONSTRAINT exec_targets_kind_check;
ALTER TABLE exec_targets ADD CHECK(kind IN('sim','kea','switch'));
ALTER TABLE exec_executions DROP CONSTRAINT exec_executions_mode_check;
ALTER TABLE exec_executions ADD CHECK(mode IN('sim','kea','fabric'));
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_runner_check;
ALTER TABLE exec_task_attempts ADD CHECK(runner IN('sim','kea','fabric'));
CREATE TABLE network_push_jobs (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 template_id varchar(32) NOT NULL REFERENCES network_template_versions(id),
 baseline_id varchar(32) NOT NULL,
 input jsonb NOT NULL,
 facts_hash varchar(64) NOT NULL,
 config_hash varchar(64) NOT NULL,
 state varchar(20) NOT NULL CHECK(state IN('CREATED','PRECHECKING','BLOCKED','READY','APPROVED','REJECTED','EXECUTING','VERIFYING','COMPLETED','PARTIAL','ROLLING_BACK','ROLLED_BACK','ARCHIVED')),
 review_hash varchar(64),
 critical boolean NOT NULL DEFAULT false,
 canary_min_size integer NOT NULL CHECK(canary_min_size BETWEEN 1 AND 500),
 push_concurrency integer NOT NULL CHECK(push_concurrency BETWEEN 1 AND 50),
 prepare_execution_id varchar(32) REFERENCES exec_executions(id),
 push_execution_id varchar(32) REFERENCES exec_executions(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE INDEX network_push_jobs_recent ON network_push_jobs(project_id,created_at DESC,id DESC);
CREATE TABLE network_push_devices (
 job_id varchar(32) NOT NULL REFERENCES network_push_jobs(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 ordinal integer NOT NULL,
 intended_id varchar(32) NOT NULL REFERENCES network_intended_configs(id),
 facts jsonb NOT NULL,
 state varchar(20) NOT NULL DEFAULT 'PENDING' CHECK(state IN('PENDING','PREPARED','BLOCKED','APPLYING','VERIFIED','PARTIAL','ROLLED_BACK','UNKNOWN','PAUSED','SKIPPED')),
 snapshot jsonb,
 running_hash varchar(64),
 snapshot_ref text,
 diff jsonb,
 prepare_evidence jsonb,
 apply_evidence jsonb,
 verify_evidence jsonb,
 code varchar(120) NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(job_id,switch_id),
 UNIQUE(job_id,ordinal)
);
CREATE TABLE network_push_signatures (
 job_id varchar(32) NOT NULL REFERENCES network_push_jobs(id),
 user_id varchar(32) NOT NULL REFERENCES users(id),
 role varchar(8) NOT NULL CHECK(role IN('NE','DE','NH')),
 review_hash varchar(64) NOT NULL,
 comment varchar(2000) NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(job_id,user_id),
 UNIQUE(job_id,role)
);
CREATE TRIGGER network_signature_immutable BEFORE UPDATE OR DELETE ON network_push_signatures FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TABLE network_push_events (
 seq bigserial PRIMARY KEY,
 job_id varchar(32) NOT NULL REFERENCES network_push_jobs(id),
 state varchar(20) NOT NULL,
 metadata jsonb NOT NULL,
 created_at timestamptz NOT NULL
);
CREATE INDEX network_push_events_job ON network_push_events(job_id,seq);
CREATE TRIGGER network_push_event_immutable BEFORE UPDATE OR DELETE ON network_push_events FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();

CREATE FUNCTION keel_network_push_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.project_id IS DISTINCT FROM NEW.project_id OR OLD.template_id IS DISTINCT FROM NEW.template_id OR OLD.baseline_id IS DISTINCT FROM NEW.baseline_id OR OLD.input IS DISTINCT FROM NEW.input OR OLD.facts_hash IS DISTINCT FROM NEW.facts_hash OR OLD.config_hash IS DISTINCT FROM NEW.config_hash OR OLD.canary_min_size IS DISTINCT FROM NEW.canary_min_size OR OLD.push_concurrency IS DISTINCT FROM NEW.push_concurrency OR OLD.created_by IS DISTINCT FROM NEW.created_by THEN RAISE EXCEPTION 'PushJob approved intent and execution policy are immutable'; END IF;
 IF OLD.state IN('READY','APPROVED','EXECUTING','VERIFYING','COMPLETED','PARTIAL','ROLLING_BACK','ROLLED_BACK','REJECTED','ARCHIVED') AND (OLD.review_hash IS DISTINCT FROM NEW.review_hash OR OLD.critical IS DISTINCT FROM NEW.critical) THEN RAISE EXCEPTION 'Approval snapshot is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER network_push_immutable BEFORE UPDATE ON network_push_jobs FOR EACH ROW EXECUTE FUNCTION keel_network_push_immutable();
