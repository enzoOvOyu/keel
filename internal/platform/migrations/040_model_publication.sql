-- Preserve complete scan payloads and seal published versions and their slots.
-- Rollback requires dropping these triggers/functions and the draft column;
-- published version identities and content hashes must never be rewritten.
ALTER TABLE device_model_drafts ADD COLUMN capability_matrix jsonb NOT NULL DEFAULT '{}';
CREATE FUNCTION reject_published_model_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'published model versions and slots are immutable'; END $$;
CREATE TRIGGER immutable_model_version BEFORE UPDATE OR DELETE ON device_model_versions FOR EACH ROW EXECUTE FUNCTION reject_published_model_mutation();
CREATE TRIGGER immutable_model_slot BEFORE UPDATE OR DELETE ON device_model_slots FOR EACH ROW EXECUTE FUNCTION reject_published_model_mutation();
CREATE FUNCTION guard_model_slot_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- Slots can only be assembled in the transaction that creates their version.
 IF NOT EXISTS(SELECT 1 FROM device_model_versions WHERE id=NEW.model_version_id AND xmin=pg_current_xact_id()::xid) THEN
  RAISE EXCEPTION 'slots cannot be appended to an already published model version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER model_slot_publication BEFORE INSERT ON device_model_slots FOR EACH ROW EXECUTE FUNCTION guard_model_slot_insert();
