-- FR-5.2: an apply row names the renderer kind it came from. The four
-- renderers share one apply table, so the diff preview and the G4 read-back
-- evidence must compare like with like — without the kind a binding apply
-- would diff against a partition render and the gate would read a foreign
-- receipt. Production carried no rows when this shipped, so no backfill is
-- needed; the CHECK keeps the four kinds exhaustive.
-- Rollback: ALTER TABLE config_set_applies DROP COLUMN kind;
-- 'template' is the WP-3.1 generic ConfigSet render; it never satisfies a
-- G4 required kind (the gate asks for the four renderer outputs).
ALTER TABLE config_set_applies ADD COLUMN kind varchar NOT NULL
  CHECK(kind IN('gpu_network','nic_partition','gpu_nic_binding','roce_import','template'));
