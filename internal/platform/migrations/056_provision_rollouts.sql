-- Immutable OS rollout intent, separate from installation/write authority.
-- Existing installations have no inferred canary status or fabricated backfill.
CREATE TABLE provision_installation_rollouts (
 installation_id varchar(32) PRIMARY KEY REFERENCES provision_installations(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 identity_sha256 char(64) NOT NULL CHECK(identity_sha256 ~ '^[a-f0-9]{64}$'),
 mode varchar(16) NOT NULL CHECK(mode IN('canary','validated')),
 canary_ids jsonb NOT NULL CHECK(jsonb_typeof(canary_ids)='array' AND octet_length(canary_ids::text)<=256),
 created_at timestamptz NOT NULL,
 CHECK((mode='canary' AND jsonb_array_length(canary_ids)=0) OR (mode='validated' AND jsonb_array_length(canary_ids) BETWEEN 1 AND 2))
);
CREATE INDEX provision_rollout_identity ON provision_installation_rollouts(project_id,identity_sha256,mode,created_at,installation_id);
CREATE FUNCTION protect_provision_rollout() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND EXISTS(SELECT 1 FROM provision_installations x WHERE x.id=NEW.installation_id AND x.project_id=NEW.project_id AND NEW.created_at>=x.created_at) THEN RETURN NEW; END IF;
 RAISE EXCEPTION 'installation rollout intent must match its owner and is append-only';
END $$;
CREATE TRIGGER protect_provision_rollout BEFORE INSERT OR UPDATE OR DELETE ON provision_installation_rollouts FOR EACH ROW EXECUTE FUNCTION protect_provision_rollout();
