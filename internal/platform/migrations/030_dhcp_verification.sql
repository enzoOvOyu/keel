-- Only a verified wire exchange can open a live bootstrap route.
ALTER TABLE ztp_kea_deployments DROP CONSTRAINT ztp_kea_deployments_state_check;
ALTER TABLE ztp_kea_deployments ADD CHECK(state IN('staged','queued','applying','config_persisted','boot_ready','blocked','unknown'));
ALTER TABLE ztp_kea_deployments ADD COLUMN dora_proof jsonb;
ALTER TABLE ztp_kea_deployments ADD COLUMN verification_code varchar(80);
ALTER TABLE ztp_kea_deployments ADD CHECK(state<>'boot_ready' OR (dora_proof IS NOT NULL AND active_hash IS NOT NULL AND verification_code='dora_verified'));
CREATE FUNCTION keel_kea_proof_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN IF OLD.dora_proof IS NOT NULL AND OLD.dora_proof IS DISTINCT FROM NEW.dora_proof THEN RAISE EXCEPTION 'DHCP proof is immutable'; END IF; RETURN NEW; END $$;
CREATE TRIGGER ztp_kea_proof_immutable BEFORE UPDATE ON ztp_kea_deployments FOR EACH ROW EXECUTE FUNCTION keel_kea_proof_immutable();
