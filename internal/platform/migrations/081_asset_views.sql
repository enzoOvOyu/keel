-- Saved inventory projections are views over existing evidence, never collectors.
CREATE TABLE asset_views(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 owner_id varchar(32) NOT NULL REFERENCES users(id),
 visibility varchar(8) NOT NULL CHECK(visibility IN('personal','team')),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE asset_view_revisions(
 view_id varchar(32) NOT NULL REFERENCES asset_views(id),
 version bigint NOT NULL,
 name varchar(100) NOT NULL,
 definition jsonb NOT NULL,
 content_hash varchar(64) NOT NULL,
 reason text NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 PRIMARY KEY(view_id,version),
 CHECK(jsonb_typeof(definition->'columns')='array' AND jsonb_array_length(definition->'columns') BETWEEN 1 AND 16)
);
ALTER TABLE asset_views ADD CONSTRAINT asset_views_revision_fk FOREIGN KEY(id,version) REFERENCES asset_view_revisions(view_id,version) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX asset_views_project ON asset_views(project_id,updated_at DESC,id DESC);
CREATE TRIGGER asset_view_revision_immutable BEFORE UPDATE OR DELETE ON asset_view_revisions FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_asset_view_owner_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id<>OLD.id OR NEW.project_id<>OLD.project_id OR NEW.owner_id<>OLD.owner_id OR NEW.visibility<>OLD.visibility OR NEW.created_at<>OLD.created_at OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'saved view identity is immutable and edits must append one revision'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER asset_view_owner_immutable BEFORE UPDATE ON asset_views FOR EACH ROW EXECUTE FUNCTION keel_asset_view_owner_immutable();
