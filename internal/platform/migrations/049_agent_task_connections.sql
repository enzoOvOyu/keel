-- Connection metadata only: execution/outbox remain the sole task source.
ALTER TABLE agent_connections
 ADD COLUMN task_session_id char(32) CHECK(task_session_id ~ '^[0-9a-f]{32}$'),
 ADD COLUMN task_connected_at timestamptz,
 ADD COLUMN task_disconnected_at timestamptz,
 ADD CHECK((task_session_id IS NULL)=(task_connected_at IS NULL)),
 ADD CHECK(task_disconnected_at IS NULL OR task_disconnected_at>=task_connected_at);
