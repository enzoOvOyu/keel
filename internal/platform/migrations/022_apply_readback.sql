-- FR-5.2: the four renderers share one read-back evidence contract. A RoCE
-- import already records before/after/read-back in its own table; the shared
-- apply row now carries the same receipt for GPU network config, NIC
-- partitioning and GPU-NIC binding, so a sampled read-back that disagrees
-- with the rendered plan is visible on the apply itself.
-- Rollback: ALTER TABLE config_set_applies DROP COLUMN receipt;
-- ALTER TABLE config_set_applies DROP COLUMN verified;
-- ALTER TABLE config_set_applies DROP COLUMN readback;
ALTER TABLE config_set_applies ADD COLUMN readback jsonb NOT NULL DEFAULT '{}';
ALTER TABLE config_set_applies ADD COLUMN verified bool NOT NULL DEFAULT false;
ALTER TABLE config_set_applies ADD COLUMN receipt text NOT NULL DEFAULT '';
