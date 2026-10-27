CREATE TABLE monitor_alerts(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 physical_port_id varchar(32) REFERENCES physical_ports(id),
 mapping_revision_id varchar(32) REFERENCES mapping_revisions(id),
 source varchar(16) NOT NULL CHECK(source IN('host','switch','bmc')),
 method varchar(16) NOT NULL,
 origin varchar(16) NOT NULL CHECK(origin IN('real','sim')),
 entity_key varchar(128) NOT NULL,
 scope_key varchar(128) NOT NULL,
 dtype varchar(2) NOT NULL,
 rule_id varchar(64) NOT NULL,
 rule_version varchar(80) NOT NULL,
 severity varchar(8) NOT NULL CHECK(severity IN('info','warning','critical')),
 state varchar(16) NOT NULL CHECK(state IN('firing','suppressed','resolved','unknown')),
 first_at timestamptz NOT NULL,
 last_at timestamptz NOT NULL,
 count bigint NOT NULL CHECK(count>0),
 version bigint NOT NULL CHECK(version>0),
 sample_sequence bigint REFERENCES monitor_metric_samples(sequence),
 issue_id varchar(32) REFERENCES issues(id),
 context jsonb NOT NULL,
 UNIQUE(project_id,machine_id,source,method,origin,scope_key,rule_id)
);
CREATE INDEX monitor_alert_list ON monitor_alerts(project_id,state,severity,last_at DESC,id DESC);
CREATE TABLE monitor_alert_events(
 id varchar(32) PRIMARY KEY,
 alert_id varchar(32) NOT NULL REFERENCES monitor_alerts(id),
 version bigint NOT NULL,
 state varchar(16) NOT NULL,
 severity varchar(8) NOT NULL,
 sample_sequence bigint REFERENCES monitor_metric_samples(sequence),
 context jsonb NOT NULL,
 observed_at timestamptz NOT NULL,
 UNIQUE(alert_id,version)
);
CREATE TRIGGER monitor_alert_event_immutable BEFORE UPDATE OR DELETE ON monitor_alert_events FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TABLE monitor_notification_outbox(
 id varchar(32) PRIMARY KEY,
 sequence bigint NOT NULL DEFAULT nextval('exec_outbox_sequence_seq') UNIQUE,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 alert_id varchar(32) NOT NULL REFERENCES monitor_alerts(id),
 aggregate_version bigint NOT NULL,
 event_type varchar(32) NOT NULL CHECK(event_type='monitor.alert'),
 severity varchar(8) NOT NULL,
 source varchar(16) NOT NULL,
 issue_id varchar(32) REFERENCES issues(id),
 created_at timestamptz NOT NULL,
 UNIQUE(alert_id,aggregate_version)
);
CREATE TRIGGER monitor_notification_immutable BEFORE UPDATE OR DELETE ON monitor_notification_outbox FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
