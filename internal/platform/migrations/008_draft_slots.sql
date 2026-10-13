-- Drafts carry their collected slots so "review and publish" prefills the
-- full inventory, not just hw_spec. Column addition only; no backfill needed
-- since prior drafts predate slot-bearing imports in the same schema family.
-- Rollback: ALTER TABLE device_model_drafts DROP COLUMN slots.
ALTER TABLE device_model_drafts ADD COLUMN slots jsonb NOT NULL DEFAULT '[]';
