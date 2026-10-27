-- Unified observational ingest. These tables grant no gate or task authority.
CREATE TABLE monitor_policies(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 stage varchar(16) NOT NULL CHECK(stage IN('M1','M2','M3','M4','maintained')),
 version bigint NOT NULL CHECK(version>0),
 policy jsonb NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id,stage,version),
 CHECK((policy->>'stage'=stage AND (policy->>'freshness_multiplier')::int=3 AND (policy->>'steady_bytes_per_second')::int BETWEEN 1024 AND 50000 AND (policy->>'window_bytes_per_second')::int BETWEEN (policy->>'steady_bytes_per_second')::int AND 200000 AND (policy->>'agentless_seconds')::int BETWEEN 300 AND 900 AND (policy->>'raw_retention_days')::int>=7 AND (policy->>'hourly_retention_days')::int>=90) IS TRUE)
);
CREATE TRIGGER monitor_policy_immutable BEFORE UPDATE OR DELETE ON monitor_policies FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TABLE monitor_assignments(
 credential_id varchar(32) PRIMARY KEY REFERENCES agent_credentials(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 connection_id varchar(32) NOT NULL REFERENCES agent_connections(id),
 assignment jsonb NOT NULL,
 assigned_at timestamptz NOT NULL
);
CREATE TABLE monitor_streams(
 credential_id varchar(32) PRIMARY KEY REFERENCES agent_credentials(id),
 connection_id varchar(32) NOT NULL REFERENCES agent_connections(id),
 last_frame_id varchar(64) NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE monitor_ingest_receipts(
 credential_id varchar(32) NOT NULL REFERENCES agent_credentials(id),
 frame_id varchar(64) NOT NULL,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 frame_hash char(64) NOT NULL,
 sample_count integer NOT NULL CHECK(sample_count BETWEEN 0 AND 64),
 received_at timestamptz NOT NULL,
 PRIMARY KEY(credential_id,frame_id)
);
CREATE TRIGGER monitor_ingest_receipt_immutable BEFORE UPDATE OR DELETE ON monitor_ingest_receipts FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TABLE monitor_metric_samples(
 sequence bigserial PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 credential_id varchar(32) REFERENCES agent_credentials(id),
 frame_id varchar(64) NOT NULL,
 metric_key varchar(64) NOT NULL,
 entity_key varchar(128) NOT NULL,
 dtype varchar(2) NOT NULL CHECK(dtype IN('D1','D2','D3','D4','D5','D6','B7')),
 source varchar(16) NOT NULL CHECK(source IN('host','switch','bmc')),
 method varchar(16) NOT NULL CHECK(method IN('agent','ssh','redfish','ipmi','collector')),
 origin varchar(16) NOT NULL CHECK(origin IN('real','sim')),
 unit varchar(16) NOT NULL,
 sample jsonb NOT NULL,
 observed_at timestamptz NOT NULL,
 received_at timestamptz NOT NULL,
 policy_id varchar(64) NOT NULL,
 interval_seconds integer NOT NULL CHECK(interval_seconds BETWEEN 10 AND 3600),
 physical_port_id varchar(32) REFERENCES physical_ports(id),
 mapping_revision_id varchar(32) REFERENCES mapping_revisions(id),
 CHECK((source='host' AND method IN('agent','ssh') AND dtype<>'B7') OR (source='bmc' AND method IN('redfish','ipmi') AND dtype='B7') OR (source='switch' AND method='collector')),
 CHECK((physical_port_id IS NULL)=(mapping_revision_id IS NULL))
);
CREATE INDEX monitor_metric_history ON monitor_metric_samples(project_id,machine_id,source,metric_key,entity_key,observed_at DESC,sequence DESC);
CREATE INDEX monitor_metric_cursor ON monitor_metric_samples(project_id,machine_id,sequence);
CREATE TABLE monitor_metric_latest(
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 source varchar(16) NOT NULL,
 method varchar(16) NOT NULL,
 metric_key varchar(64) NOT NULL,
 entity_key varchar(128) NOT NULL,
 sample_sequence bigint NOT NULL REFERENCES monitor_metric_samples(sequence),
 PRIMARY KEY(project_id,machine_id,source,method,metric_key,entity_key)
);
CREATE TABLE monitor_rate_buckets(
 id varchar(80) PRIMARY KEY,
 tokens double precision NOT NULL CHECK(tokens>=0),
 updated_at timestamptz NOT NULL
);
CREATE TABLE monitor_hourly_samples(
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 source varchar(16) NOT NULL,
 method varchar(16) NOT NULL,
 origin varchar(16) NOT NULL,
 metric_key varchar(64) NOT NULL,
 entity_key varchar(128) NOT NULL,
 bucket_at timestamptz NOT NULL,
 unit varchar(16) NOT NULL,
 samples bigint NOT NULL,
 good_samples bigint NOT NULL,
 expected_samples bigint NOT NULL,
 minimum double precision,
 maximum double precision,
 total double precision,
 last_sequence bigint NOT NULL,
 PRIMARY KEY(project_id,machine_id,source,method,origin,metric_key,entity_key,bucket_at)
);

CREATE TABLE monitor_gaps(
 sequence bigserial PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 credential_id varchar(32) REFERENCES agent_credentials(id),
 frame_id varchar(64) NOT NULL,
 source varchar(16) NOT NULL,
 method varchar(16) NOT NULL,
 started_at timestamptz NOT NULL,
 ended_at timestamptz NOT NULL CHECK(ended_at>=started_at),
 reason varchar(64) NOT NULL
);

CREATE TABLE monitor_assignment_versions(
 credential_id varchar(32) NOT NULL REFERENCES agent_credentials(id),
 policy_id varchar(64) NOT NULL,
 assignment jsonb NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(credential_id,policy_id)
);
CREATE TRIGGER monitor_assignment_version_immutable BEFORE UPDATE OR DELETE ON monitor_assignment_versions FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
