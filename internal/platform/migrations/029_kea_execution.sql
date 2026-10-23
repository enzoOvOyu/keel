-- A shared target registry keeps control services out of simulated inventory.
CREATE TABLE exec_targets (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 kind varchar(8) NOT NULL CHECK(kind IN('sim','kea')),
 resource_key varchar(64),
 UNIQUE(project_id,kind,resource_key)
);
INSERT INTO exec_targets(id,project_id,kind) SELECT id,project_id,'sim' FROM exec_sim_targets;
CREATE FUNCTION keel_register_sim_target() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO exec_targets(id,project_id,kind) VALUES(NEW.id,NEW.project_id,'sim'); RETURN NEW; END $$;
CREATE TRIGGER exec_sim_target_registry AFTER INSERT ON exec_sim_targets FOR EACH ROW EXECUTE FUNCTION keel_register_sim_target();
ALTER TABLE exec_execution_targets DROP CONSTRAINT exec_execution_targets_target_id_fkey;
ALTER TABLE exec_execution_targets ADD FOREIGN KEY(target_id) REFERENCES exec_targets(id);
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_target_id_fkey;
ALTER TABLE exec_task_attempts ADD FOREIGN KEY(target_id) REFERENCES exec_targets(id);
ALTER TABLE exec_executions DROP CONSTRAINT exec_executions_mode_check;
ALTER TABLE exec_executions ADD CHECK(mode IN('sim','kea'));
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_runner_check;
ALTER TABLE exec_task_attempts ADD CHECK(runner IN('sim','kea'));
CREATE TABLE ztp_kea_deployments (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 execution_id varchar(32) UNIQUE REFERENCES exec_executions(id),
 state varchar(24) NOT NULL CHECK(state IN('staged','queued','applying','config_persisted','blocked','unknown')),
 spec jsonb NOT NULL,
 spec_hash varchar(64) NOT NULL,
 before_hash varchar(64) NOT NULL,
 candidate_hash varchar(64) NOT NULL,
 active_hash varchar(64),
 failure_code varchar(80),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE INDEX ztp_kea_deployments_recent ON ztp_kea_deployments(project_id,created_at DESC,id DESC);
CREATE FUNCTION keel_kea_snapshot_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF OLD.spec IS DISTINCT FROM NEW.spec OR OLD.spec_hash IS DISTINCT FROM NEW.spec_hash OR OLD.before_hash IS DISTINCT FROM NEW.before_hash OR OLD.candidate_hash IS DISTINCT FROM NEW.candidate_hash OR OLD.project_id IS DISTINCT FROM NEW.project_id THEN RAISE EXCEPTION 'Kea deployment snapshot is immutable'; END IF; RETURN NEW; END $$;
CREATE TRIGGER ztp_kea_snapshot_immutable BEFORE UPDATE ON ztp_kea_deployments FOR EACH ROW EXECUTE FUNCTION keel_kea_snapshot_immutable();
ALTER TABLE exec_worker_heartbeats DROP CONSTRAINT exec_worker_heartbeats_mode_check;
ALTER TABLE exec_worker_heartbeats ADD CHECK(mode IN('sim','mixed'));
