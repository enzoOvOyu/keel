CREATE TABLE network_rule_versions (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 rule_id varchar(64) NOT NULL,
 domain varchar(16) NOT NULL CHECK(domain='lossless'),
 version bigint NOT NULL CHECK(version>1),
 state varchar(16) NOT NULL CHECK(state IN('draft','released','disabled')),
 target_state varchar(16) NOT NULL CHECK(target_state IN('released','disabled')),
 definition jsonb NOT NULL,
 metric_unit varchar(32) NOT NULL,
 scope_hash varchar(64) NOT NULL,
 content_hash varchar(64) NOT NULL,
 preview_hash varchar(64) NOT NULL,
 preview_report jsonb NOT NULL,
 change_reason varchar(1000) NOT NULL,
 override_reason varchar(1000) NOT NULL DEFAULT '',
 supersedes_id varchar(32) REFERENCES network_rule_versions(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 published_by varchar(32) REFERENCES users(id),
 published_at timestamptz,
 UNIQUE(project_id,rule_id,version)
);
CREATE INDEX network_rules_project ON network_rule_versions(project_id,rule_id,version DESC);
CREATE TABLE network_rule_approvals (
 id varchar(32) PRIMARY KEY,
 version_id varchar(32) NOT NULL REFERENCES network_rule_versions(id),
 user_id varchar(32) NOT NULL REFERENCES users(id),
 approval_role varchar(8) NOT NULL CHECK(approval_role IN('NE','NH')),
 authority_role varchar(16) NOT NULL,
 security_version integer NOT NULL,
 preview_hash varchar(64) NOT NULL,
 created_at timestamptz NOT NULL,
 UNIQUE(version_id,user_id),
 UNIQUE(version_id,approval_role)
);
CREATE TRIGGER network_rule_approval_immutable BEFORE UPDATE OR DELETE ON network_rule_approvals FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_network_rule_version_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Rule versions cannot be deleted'; END IF;
 IF (to_jsonb(OLD)-'state'-'published_by'-'published_at') IS DISTINCT FROM (to_jsonb(NEW)-'state'-'published_by'-'published_at') THEN RAISE EXCEPTION 'Rule definition and preview evidence are immutable; create a new version'; END IF;
 IF OLD.state<>'draft' OR NEW.state<>OLD.target_state OR NEW.published_by IS NULL OR NEW.published_at IS NULL THEN RAISE EXCEPTION 'Only an approved draft can be published once'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER network_rule_version_immutable BEFORE UPDATE OR DELETE ON network_rule_versions FOR EACH ROW EXECUTE FUNCTION keel_network_rule_version_immutable();
