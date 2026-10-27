-- Network precheck discoveries share WP-2.7's issue pool. Device and protocol
-- fixture evidence have distinct identities and never close each other's issues.
CREATE TABLE fabric_issue_bindings (
 issue_id varchar(32) PRIMARY KEY REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 cable_uid varchar(32) NOT NULL REFERENCES bom_items(item_uid),
 category varchar(2) NOT NULL CHECK(category IN('T1','T2','T3','T4','T6')),
 evidence_origin varchar(20) NOT NULL CHECK(evidence_origin IN('device','protocol_fixture')),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id,cable_uid,category,evidence_origin),
 UNIQUE(issue_id,project_id,evidence_origin)
);
CREATE TABLE fabric_issue_observations (
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL,
 project_id varchar(32) NOT NULL,
 evidence_origin varchar(20) NOT NULL,
 run_id varchar(32) NOT NULL REFERENCES fabric_collection_runs(id),
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id),
 observation_hash char(64) NOT NULL CHECK(observation_hash ~ '^[0-9a-f]{64}$'),
 finding jsonb NOT NULL,
 observed_at timestamptz NOT NULL,
 valid_until timestamptz NOT NULL CHECK(valid_until>observed_at),
 recorded_at timestamptz NOT NULL,
 UNIQUE(issue_id,run_id),
 FOREIGN KEY(issue_id,project_id,evidence_origin) REFERENCES fabric_issue_bindings(issue_id,project_id,evidence_origin)
);
CREATE INDEX fabric_issue_observation_history ON fabric_issue_observations(issue_id,recorded_at DESC,id DESC);
CREATE INDEX fabric_issue_observation_identity ON fabric_issue_observations(issue_id,observation_hash);
CREATE INDEX fabric_issue_observation_run ON fabric_issue_observations(run_id,issue_id);
CREATE FUNCTION keel_fabric_issue_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='fabric_issue_bindings' THEN
  IF NOT EXISTS(SELECT 1 FROM issues i WHERE i.id=NEW.issue_id AND i.project_id=NEW.project_id
    AND i.source='fabric' AND i.phase='delivery' AND i.category=NEW.category
    AND i.anchors @> jsonb_build_array(jsonb_build_object('kind','cable','id',NEW.cable_uid))) THEN
   RAISE EXCEPTION 'Network issue discovery scope does not match the original issue';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM fabric_collection_runs r WHERE r.id=NEW.run_id AND r.project_id=NEW.project_id
    AND r.package_id=NEW.package_id AND r.report->>'evidence_source'=NEW.evidence_origin
    AND NEW.valid_until<=r.valid_until)
   OR NOT EXISTS(SELECT 1 FROM fabric_issue_bindings b WHERE b.issue_id=NEW.issue_id
    AND NEW.finding->>'cable_uid'=b.cable_uid AND NEW.finding->>'issue_type'=b.category
    AND NEW.finding->>'verdict'='anomaly' AND NEW.finding->>'source'='switch') THEN
   RAISE EXCEPTION 'Network observation must match its project, mode, run and discovery';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER check_fabric_issue_binding BEFORE INSERT ON fabric_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_fabric_issue_scope();
CREATE TRIGGER check_fabric_issue_observation BEFORE INSERT ON fabric_issue_observations FOR EACH ROW EXECUTE FUNCTION keel_fabric_issue_scope();
CREATE TRIGGER immutable_fabric_issue_binding BEFORE UPDATE OR DELETE ON fabric_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER immutable_fabric_issue_observation BEFORE UPDATE OR DELETE ON fabric_issue_observations FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
