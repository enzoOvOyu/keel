-- Simulation-only execution kernel. No Machine/ProjectAsset or physical network adapters.
CREATE SEQUENCE exec_fencing_tokens;
CREATE TABLE exec_sim_targets (
 id varchar(32) PRIMARY KEY, project_id varchar(32) NOT NULL REFERENCES projects(id),
 name varchar(120) NOT NULL, seed bigint NOT NULL, revision bigint NOT NULL DEFAULT 1,
 model_version varchar NOT NULL DEFAULT 'sim-model-v1', created_at timestamptz NOT NULL,
 UNIQUE(project_id,name)
);
CREATE TABLE exec_flows (
 id varchar(32) PRIMARY KEY, project_id varchar(32) NOT NULL REFERENCES projects(id),
 name varchar(120) NOT NULL, version bigint NOT NULL DEFAULT 1,
 created_by varchar(32) NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 UNIQUE(project_id,name)
);
CREATE TABLE exec_flow_versions (
 id varchar(32) PRIMARY KEY, flow_id varchar(32) NOT NULL REFERENCES exec_flows(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id), version bigint NOT NULL,
 definition jsonb NOT NULL, yaml_source text NOT NULL DEFAULT '', content_hash varchar(64) NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL,
 UNIQUE(flow_id,version)
);
CREATE TABLE exec_executions (
 id varchar(32) PRIMARY KEY, project_id varchar(32) NOT NULL REFERENCES projects(id),
 flow_version_id varchar(32) NOT NULL REFERENCES exec_flow_versions(id), mode varchar NOT NULL DEFAULT 'sim' CHECK(mode='sim'),
 state varchar NOT NULL DEFAULT 'created', state_version bigint NOT NULL DEFAULT 1,
 desired_state varchar NOT NULL DEFAULT 'running' CHECK(desired_state IN('running','paused','killed')),
 parameters jsonb NOT NULL, snapshot jsonb NOT NULL, target_set_hash varchar(64) NOT NULL,
 concurrency integer NOT NULL CHECK(concurrency BETWEEN 1 AND 256), circuit_breaker_percent integer NOT NULL DEFAULT 5 CHECK(circuit_breaker_percent BETWEEN 1 AND 100),
 breaker_ack_failures integer NOT NULL DEFAULT 0,
 created_by varchar(32) NOT NULL REFERENCES users(id), created_at timestamptz NOT NULL,
 started_at timestamptz, finished_at timestamptz,
 CHECK(state IN('created','scheduled','dispatching','running','pausing','paused','cancelling','reconciling','success','failed','killed','warning'))
);
CREATE INDEX exec_executions_project ON exec_executions(project_id,created_at,id);
CREATE TABLE exec_execution_targets (
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id), target_id varchar(32) NOT NULL REFERENCES exec_sim_targets(id),
 snapshot jsonb NOT NULL, PRIMARY KEY(execution_id,target_id)
);
CREATE TABLE exec_task_attempts (
 id varchar(32) PRIMARY KEY, execution_id varchar(32) NOT NULL REFERENCES exec_executions(id), target_id varchar(32) NOT NULL REFERENCES exec_sim_targets(id),
 task_id varchar(64) NOT NULL, task_index integer NOT NULL, attempt_no integer NOT NULL,
 command_id varchar(32) NOT NULL UNIQUE, runner varchar NOT NULL DEFAULT 'sim' CHECK(runner='sim'),
 state varchar NOT NULL DEFAULT 'pending' CHECK(state IN('pending','running','success','failed','cancelled','unknown','skipped')),
 state_version bigint NOT NULL DEFAULT 1, lease_owner varchar(120), lease_until timestamptz, fencing_token bigint,
 result jsonb, started_at timestamptz, finished_at timestamptz, created_at timestamptz NOT NULL,
 UNIQUE(execution_id,target_id,task_id,attempt_no)
);
CREATE INDEX exec_attempts_dispatch ON exec_task_attempts(state,execution_id,task_index);
CREATE TABLE exec_resources (id varchar(160) PRIMARY KEY);
CREATE TABLE exec_resource_leases (
 resource_id varchar(160) NOT NULL REFERENCES exec_resources(id), attempt_id varchar(32) NOT NULL REFERENCES exec_task_attempts(id),
 mode varchar NOT NULL CHECK(mode IN('shared','exclusive')), fencing_token bigint NOT NULL,
 state varchar NOT NULL CHECK(state IN('held','reconciling','released')), expires_at timestamptz NOT NULL,
 PRIMARY KEY(resource_id,attempt_id)
);
CREATE INDEX exec_resources_active ON exec_resource_leases(resource_id,state);
CREATE TABLE exec_runner_receipts (
 command_id varchar(32) PRIMARY KEY REFERENCES exec_task_attempts(command_id), attempt_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(id),
 fencing_token bigint NOT NULL, result varchar NOT NULL CHECK(result IN('success','failed','cancelled')),
 payload jsonb NOT NULL, digest varchar(64) NOT NULL, observed_at timestamptz NOT NULL
);
CREATE TABLE exec_outbox (
 sequence bigserial UNIQUE, id varchar(32) PRIMARY KEY, execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 aggregate_version bigint NOT NULL, event_type varchar NOT NULL, payload jsonb NOT NULL, created_at timestamptz NOT NULL, published_at timestamptz
);
CREATE INDEX exec_outbox_unpublished ON exec_outbox(sequence) WHERE published_at IS NULL;
CREATE TABLE exec_consumed_events (consumer varchar(120) NOT NULL,event_id varchar(32) NOT NULL REFERENCES exec_outbox(id),handled_at timestamptz NOT NULL,PRIMARY KEY(consumer,event_id));
CREATE TABLE exec_mutations (
 id varchar(64) PRIMARY KEY, request_hash varchar(64) NOT NULL, response jsonb NOT NULL, created_at timestamptz NOT NULL
);
CREATE FUNCTION keel_exec_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'execution evidence is immutable'; END $$;
CREATE TRIGGER exec_flow_version_immutable BEFORE UPDATE OR DELETE ON exec_flow_versions FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER exec_target_snapshot_immutable BEFORE UPDATE OR DELETE ON exec_execution_targets FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER exec_receipt_immutable BEFORE UPDATE OR DELETE ON exec_runner_receipts FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_exec_snapshot_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF OLD.snapshot IS DISTINCT FROM NEW.snapshot OR OLD.parameters IS DISTINCT FROM NEW.parameters OR OLD.target_set_hash IS DISTINCT FROM NEW.target_set_hash OR OLD.flow_version_id IS DISTINCT FROM NEW.flow_version_id OR OLD.project_id IS DISTINCT FROM NEW.project_id THEN RAISE EXCEPTION 'execution snapshot is immutable'; END IF; RETURN NEW; END $$;
CREATE TRIGGER exec_snapshot_immutable BEFORE UPDATE ON exec_executions FOR EACH ROW EXECUTE FUNCTION keel_exec_snapshot_immutable();
CREATE TABLE exec_worker_heartbeats(id varchar(120) PRIMARY KEY,mode varchar NOT NULL CHECK(mode='sim'),active_count integer NOT NULL,observed_at timestamptz NOT NULL);
