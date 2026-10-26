-- Public workflow metadata only; never credentials, device configuration or
-- hardware evidence. Values and command receipts commit in one transaction.
ALTER TABLE exec_targets DROP CONSTRAINT exec_targets_kind_check;
ALTER TABLE exec_targets ADD CONSTRAINT exec_targets_kind_check CHECK(kind IN('sim','kea','switch','machine','project'));
ALTER TABLE exec_executions DROP CONSTRAINT exec_executions_mode_check;
ALTER TABLE exec_executions ADD CONSTRAINT exec_executions_mode_check CHECK(mode IN('sim','kea','fabric','ssh','agent','provision','control'));
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_runner_check;
ALTER TABLE exec_task_attempts ADD CONSTRAINT exec_task_attempts_runner_check CHECK(runner IN('sim','kea','fabric','ssh','agent','provision','control'));
CREATE TABLE exec_project_kv (
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 key varchar(64) NOT NULL,
 value varchar(4096) NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 updated_by_command varchar(32) NOT NULL REFERENCES exec_task_attempts(command_id),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,key),
 CHECK(key ~ '^[a-z][a-z0-9_.-]{0,63}$')
);
