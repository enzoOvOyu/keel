CREATE TABLE ztp_replacements (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 from_session_id varchar(32) NOT NULL REFERENCES ztp_sessions(id),
 from_switch_id varchar(32) NOT NULL REFERENCES switches(id),
 to_switch_id varchar(32) NOT NULL REFERENCES switches(id),
 mode varchar(12) NOT NULL CHECK(mode IN('sim','device')),
 state varchar(12) NOT NULL CHECK(state IN('requested','approved','rejected')),
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>0),
 snapshot jsonb NOT NULL,
 snapshot_sha256 varchar(64) NOT NULL CHECK(snapshot_sha256 ~ '^[a-f0-9]{64}$'),
 reason varchar(1000) NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 approved_by varchar(32) REFERENCES users(id),
 approved_at timestamptz,
 CHECK(from_switch_id<>to_switch_id),
 CHECK(approved_by IS NULL OR approved_by<>created_by),
 CHECK((state='approved')=(approved_by IS NOT NULL AND approved_at IS NOT NULL))
);
CREATE FUNCTION keel_ztp_replacement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'replacement history is immutable'; END IF;
 IF OLD.state<>'requested' OR NEW.state NOT IN('approved','rejected') OR NEW.state_version<>OLD.state_version+1
 OR ROW(NEW.id,NEW.project_id,NEW.from_session_id,NEW.from_switch_id,NEW.to_switch_id,NEW.mode,NEW.snapshot,NEW.snapshot_sha256,NEW.reason,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.from_session_id,OLD.from_switch_id,OLD.to_switch_id,OLD.mode,OLD.snapshot,OLD.snapshot_sha256,OLD.reason,OLD.created_by,OLD.created_at)
 THEN RAISE EXCEPTION 'replacement snapshot and final decision are immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER ztp_replacement_immutable BEFORE UPDATE OR DELETE ON ztp_replacements FOR EACH ROW EXECUTE FUNCTION keel_ztp_replacement_guard();
CREATE INDEX ztp_replacements_project ON ztp_replacements(project_id,created_at DESC,id DESC);
CREATE UNIQUE INDEX ztp_replacements_pending_old ON ztp_replacements(project_id,mode,from_switch_id) WHERE state='requested';
CREATE UNIQUE INDEX ztp_replacements_pending_new ON ztp_replacements(project_id,mode,to_switch_id) WHERE state='requested';
CREATE TABLE ztp_switch_project_bindings (
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 mode varchar(12) NOT NULL CHECK(mode IN('sim','device')),
 state varchar(12) NOT NULL CHECK(state IN('active','retired')),
 replacement_id varchar(32) NOT NULL REFERENCES ztp_replacements(id),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,switch_id,mode)
);
ALTER TABLE ztp_identity_history ADD COLUMN replacement_id varchar(32) REFERENCES ztp_replacements(id);
-- Historical declarations predate this approval workflow. Keep them as facts,
-- without treating them as approved retirement authorizations.
