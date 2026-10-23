-- Multi-replica retry workers select only due simulated failures.
CREATE INDEX ztp_sessions_sim_retry_due ON ztp_sessions(next_retry_at,id)
 WHERE mode='sim' AND state='failed' AND next_retry_at IS NOT NULL;
