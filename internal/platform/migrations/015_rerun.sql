-- FR-1.4 batch semantics: target labels for group selection (rack/rail/BOM
-- group/hand-picked translate to label selectors) and rerun support. The
-- rerun state lives on the execution: a rerun re-opens finished executions
-- for the selected targets by inserting new attempt chains; the execution
-- flips back to dispatching until the new attempts settle.
-- Rollback: ALTER TABLE exec_sim_targets DROP COLUMN labels;
--           ALTER TABLE exec_executions DROP COLUMN rerun_of, DROP COLUMN rerun_scope.
ALTER TABLE exec_sim_targets ADD COLUMN labels jsonb NOT NULL DEFAULT '{}';
ALTER TABLE exec_executions ADD COLUMN rerun_of varchar(32) REFERENCES exec_executions(id);
ALTER TABLE exec_executions ADD COLUMN rerun_scope jsonb NOT NULL DEFAULT '[]';
