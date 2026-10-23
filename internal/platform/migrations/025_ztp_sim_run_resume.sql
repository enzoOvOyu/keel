-- Durable, simulation-only inputs permit a retry worker to resume the same gates.
ALTER TABLE ztp_sessions ADD COLUMN sim_run_input jsonb;
ALTER TABLE ztp_sessions ADD CONSTRAINT ztp_sim_run_input_only CHECK (mode='sim' OR sim_run_input IS NULL);
