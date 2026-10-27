-- A mutable alert is not the discovery identity of a physical repair.
CREATE TABLE portmon_issue_bindings (
 issue_id varchar(32) PRIMARY KEY REFERENCES issues(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 asset_version bigint NOT NULL CHECK(asset_version>0),
 physical_port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 mapping_revision_id varchar(32) NOT NULL REFERENCES mapping_revisions(id),
 alert_id varchar(32) NOT NULL REFERENCES monitor_alerts(id),
 sample_sequence bigint NOT NULL REFERENCES monitor_metric_samples(sequence),
 rule_id varchar(64) NOT NULL,
 rule_version text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(issue_id,sample_sequence)
);
CREATE INDEX portmon_issue_bindings_port ON portmon_issue_bindings(project_id,physical_port_id);
CREATE TRIGGER portmon_issue_binding_immutable BEFORE UPDATE OR DELETE ON portmon_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_portmon_issue_binding_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM issues issue JOIN project_assets asset ON asset.project_id=issue.project_id JOIN monitor_alerts alert ON alert.issue_id=issue.id JOIN monitor_metric_samples sample ON sample.sequence=alert.sample_sequence JOIN mapping_revisions revision ON revision.id=sample.mapping_revision_id JOIN mapping_assessments assessment ON assessment.id=revision.assessment_id JOIN monitor_alert_events event ON event.alert_id=alert.id AND event.version=alert.version
 WHERE issue.id=NEW.issue_id AND issue.source='portmon' AND issue.phase='delivery' AND issue.category=CASE WHEN NEW.rule_id='link-down' THEN 'T1' WHEN NEW.rule_id='link-flapping' THEN 'T4' WHEN NEW.rule_id IN('optics-rx_dbm','optics-tx_dbm','optics-temperature_c') THEN 'T3' WHEN NEW.rule_id='pfc-pause-growth' THEN 'T6' END AND issue.project_id=NEW.project_id AND asset.id=NEW.asset_id AND asset.machine_id=NEW.machine_id AND asset.version=NEW.asset_version AND alert.id=NEW.alert_id AND alert.machine_id=NEW.machine_id AND alert.physical_port_id=NEW.physical_port_id AND alert.mapping_revision_id=NEW.mapping_revision_id AND alert.rule_id=NEW.rule_id AND alert.rule_version=NEW.rule_version AND alert.state='firing' AND alert.severity='critical' AND event.sample_sequence=NEW.sample_sequence AND event.state='firing' AND sample.sequence=NEW.sample_sequence AND sample.project_id=NEW.project_id AND sample.machine_id=NEW.machine_id AND sample.physical_port_id=NEW.physical_port_id AND sample.mapping_revision_id=NEW.mapping_revision_id AND sample.origin='real' AND sample.sample->>'quality'='good' AND revision.asset_id=NEW.asset_id AND revision.project_id=NEW.project_id AND EXISTS(SELECT 1 FROM jsonb_array_elements(assessment.candidate->'bindings') binding WHERE binding#>>'{plan,physical_port_id}'=NEW.physical_port_id AND binding#>>'{plan,role}'<>'unused') AND EXISTS(SELECT 1 FROM issue_events created WHERE created.issue_id=issue.id AND created.action='create' AND created.data->>'origin'='monitor_rule' AND created.data->>'sample_sequence'=NEW.sample_sequence::text)) THEN
 RAISE EXCEPTION 'portmon repair requires its exact original physical discovery sample and rule';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER portmon_issue_binding_scope BEFORE INSERT ON portmon_issue_bindings FOR EACH ROW EXECUTE FUNCTION keel_portmon_issue_binding_scope();
