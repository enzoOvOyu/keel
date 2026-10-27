-- Durable release work list. A row is a request to assess a template upgrade,
-- not an observed hardware fault, an approval or a production mutation.
CREATE TABLE network_template_changes (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 candidate_template_id varchar(32) NOT NULL REFERENCES network_template_versions(id),
 base_template_id varchar(32) NOT NULL REFERENCES network_template_versions(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 source_job_id varchar(32) NOT NULL REFERENCES network_push_jobs(id),
 source_execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(candidate_template_id,switch_id,source_execution_id)
);
CREATE INDEX network_template_changes_recent ON network_template_changes(project_id,created_at DESC,id DESC);
CREATE TABLE network_template_change_jobs (
 change_id varchar(32) NOT NULL REFERENCES network_template_changes(id),
 job_id varchar(32) NOT NULL REFERENCES network_push_jobs(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 PRIMARY KEY(change_id,job_id)
);
CREATE TRIGGER network_template_changes_immutable BEFORE UPDATE OR DELETE ON network_template_changes FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER network_template_change_jobs_immutable BEFORE UPDATE OR DELETE ON network_template_change_jobs FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_network_template_change_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE candidate network_template_versions%ROWTYPE; base network_template_versions%ROWTYPE; job network_push_jobs%ROWTYPE; change network_template_changes%ROWTYPE;
BEGIN
 IF TG_TABLE_NAME='network_template_changes' THEN
  SELECT * INTO STRICT candidate FROM network_template_versions WHERE id=NEW.candidate_template_id;
  SELECT * INTO STRICT base FROM network_template_versions WHERE id=NEW.base_template_id;
  SELECT * INTO STRICT job FROM network_push_jobs WHERE id=NEW.source_job_id;
  IF candidate.project_id<>NEW.project_id OR base.project_id<>NEW.project_id OR candidate.name<>base.name OR candidate.version<=base.version OR candidate.state<>'released' OR job.project_id<>NEW.project_id OR job.template_id<>base.id OR job.push_execution_id IS DISTINCT FROM NEW.source_execution_id OR NOT EXISTS(SELECT 1 FROM network_push_devices WHERE job_id=job.id AND switch_id=NEW.switch_id) THEN RAISE EXCEPTION 'Template change references must share release, project, family and source device'; END IF;
 ELSE
  SELECT * INTO STRICT change FROM network_template_changes WHERE id=NEW.change_id;
  SELECT * INTO STRICT job FROM network_push_jobs WHERE id=NEW.job_id;
  IF job.project_id<>change.project_id OR job.template_id<>change.candidate_template_id OR NOT EXISTS(SELECT 1 FROM network_push_devices WHERE job_id=job.id AND switch_id=change.switch_id) THEN RAISE EXCEPTION 'Template change job must contain the same project, candidate and device'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER network_template_change_scope BEFORE INSERT ON network_template_changes FOR EACH ROW EXECUTE FUNCTION keel_network_template_change_scope();
CREATE TRIGGER network_template_change_job_scope BEFORE INSERT ON network_template_change_jobs FOR EACH ROW EXECUTE FUNCTION keel_network_template_change_scope();
