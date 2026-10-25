-- Opt-in bounded server SSH tasks use the existing target registry, execution
-- outbox, attempts, resource leases and durable receipts. No secrets in SQL.
ALTER TABLE exec_targets DROP CONSTRAINT exec_targets_kind_check;
ALTER TABLE exec_targets ADD CHECK(kind IN('sim','kea','switch','machine'));
ALTER TABLE exec_executions DROP CONSTRAINT exec_executions_mode_check;
ALTER TABLE exec_executions ADD CHECK(mode IN('sim','kea','fabric','ssh'));
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_runner_check;
ALTER TABLE exec_task_attempts ADD CHECK(runner IN('sim','kea','fabric','ssh'));
