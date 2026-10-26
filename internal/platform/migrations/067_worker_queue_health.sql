-- A DB-only heartbeat cannot establish configured Redis delivery health.
ALTER TABLE exec_worker_heartbeats ADD COLUMN queue_mode varchar(32) NOT NULL
 DEFAULT 'postgresql-only' CHECK(queue_mode IN ('postgresql-only','redis-streams'));
