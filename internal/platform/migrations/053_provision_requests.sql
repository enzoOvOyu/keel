-- Frozen installation preparation only. No DHCP, host state or trust projection.
ALTER TABLE exec_executions DROP CONSTRAINT exec_executions_mode_check;
ALTER TABLE exec_executions ADD CONSTRAINT exec_executions_mode_check CHECK(mode IN('sim','kea','fabric','ssh','agent','provision'));
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_runner_check;
ALTER TABLE exec_task_attempts ADD CONSTRAINT exec_task_attempts_runner_check CHECK(runner IN('sim','kea','fabric','ssh','agent','provision'));
CREATE TABLE provision_requests (
 id varchar(32) PRIMARY KEY CHECK(id ~ '^[a-f0-9]{32}$'),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason varchar(500) NOT NULL CHECK(length(reason)>0),
 reinstall boolean NOT NULL,
 snapshot jsonb NOT NULL,
 snapshot_sha256 char(64) NOT NULL CHECK(snapshot_sha256 ~ '^[a-f0-9]{64}$'),
 state varchar NOT NULL DEFAULT 'requested' CHECK(state IN('requested','approved','preparing','prepared','failed','canceled','expired')),
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>=1),
 approved_by varchar(32) REFERENCES users(id),
 approved_at timestamptz,
 execution_id varchar(32) UNIQUE REFERENCES exec_executions(id),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 updated_at timestamptz NOT NULL,
 CHECK((approved_by IS NULL)=(approved_at IS NULL)),
 CHECK(approved_by IS NULL OR approved_by<>created_by),
 CHECK(state NOT IN('approved','preparing','prepared') OR approved_by IS NOT NULL),
 CHECK(state NOT IN('preparing','prepared') OR execution_id IS NOT NULL)
);
CREATE INDEX provision_requests_project ON provision_requests(project_id,created_at DESC,id);
CREATE UNIQUE INDEX provision_one_active_machine ON provision_requests(machine_id) WHERE state IN('requested','approved','preparing','prepared');
CREATE FUNCTION protect_provision_request() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'installation request evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.machine_id,NEW.asset_id,NEW.created_by,NEW.reason,NEW.reinstall,NEW.snapshot,NEW.snapshot_sha256,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.machine_id,OLD.asset_id,OLD.created_by,OLD.reason,OLD.reinstall,OLD.snapshot,OLD.snapshot_sha256,OLD.created_at,OLD.expires_at)
 OR NEW.state_version<>OLD.state_version+1 OR OLD.state IN('failed','canceled','expired')
 OR NOT((OLD.state='requested' AND NEW.state IN('approved','canceled','expired'))
 OR (OLD.state='approved' AND NEW.state IN('preparing','canceled','expired'))
 OR (OLD.state='preparing' AND NEW.state IN('prepared','failed','canceled'))
 OR (OLD.state='prepared' AND NEW.state IN('canceled','expired'))) THEN
  RAISE EXCEPTION 'installation request identity and transitions are immutable';
 END IF;
 IF OLD.approved_by IS NULL AND NEW.approved_by IS NOT NULL AND NOT(OLD.state='requested' AND NEW.state='approved') THEN RAISE EXCEPTION 'approval may only be assigned during approval'; END IF;
 IF OLD.execution_id IS NULL AND NEW.execution_id IS NOT NULL AND NOT(OLD.state='approved' AND NEW.state='preparing') THEN RAISE EXCEPTION 'execution may only be assigned during consumption'; END IF;
 IF OLD.approved_by IS NOT NULL AND ROW(NEW.approved_by,NEW.approved_at) IS DISTINCT FROM ROW(OLD.approved_by,OLD.approved_at) THEN RAISE EXCEPTION 'request approval is immutable'; END IF;
 IF OLD.execution_id IS NOT NULL AND NEW.execution_id IS DISTINCT FROM OLD.execution_id THEN RAISE EXCEPTION 'request execution is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_provision_request BEFORE UPDATE OR DELETE ON provision_requests FOR EACH ROW EXECUTE FUNCTION protect_provision_request();
