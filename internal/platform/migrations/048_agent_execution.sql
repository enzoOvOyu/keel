-- Agent tasks reuse the same attempts, outbox, resource leases and receipts.
-- No Agent-specific execution queue or alternate resource authority is created.
ALTER TABLE exec_executions DROP CONSTRAINT exec_executions_mode_check;
ALTER TABLE exec_executions ADD CHECK(mode IN('sim','kea','fabric','ssh','agent'));
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_runner_check;
ALTER TABLE exec_task_attempts ADD CHECK(runner IN('sim','kea','fabric','ssh','agent'));
