CREATE TABLE network_template_versions (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 name varchar(120) NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 state varchar(16) NOT NULL CHECK(state IN('draft','tested','released','deprecated')),
 source_ref varchar(300) NOT NULL,
 definition jsonb NOT NULL,
 content_hash varchar(64) NOT NULL,
 test_report jsonb,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 released_by varchar(32) REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(project_id,name,version)
);
CREATE INDEX network_template_versions_recent ON network_template_versions(project_id,created_at DESC,id DESC);
CREATE FUNCTION keel_network_template_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.definition IS DISTINCT FROM NEW.definition OR OLD.content_hash IS DISTINCT FROM NEW.content_hash OR OLD.project_id IS DISTINCT FROM NEW.project_id OR OLD.name IS DISTINCT FROM NEW.name OR OLD.version IS DISTINCT FROM NEW.version OR OLD.source_ref IS DISTINCT FROM NEW.source_ref THEN RAISE EXCEPTION 'Network template version is immutable; create a new version'; END IF;
 IF OLD.state<>'draft' AND OLD.test_report IS DISTINCT FROM NEW.test_report THEN RAISE EXCEPTION 'Template test evidence is immutable'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER network_template_immutable BEFORE UPDATE ON network_template_versions FOR EACH ROW EXECUTE FUNCTION keel_network_template_immutable();
CREATE TABLE network_intended_configs (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 template_id varchar(32) NOT NULL REFERENCES network_template_versions(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 content_hash varchar(64) NOT NULL,
 configuration jsonb NOT NULL,
 created_at timestamptz NOT NULL,
 UNIQUE(template_id,switch_id,content_hash)
);
CREATE TRIGGER network_intended_immutable BEFORE UPDATE OR DELETE ON network_intended_configs FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
