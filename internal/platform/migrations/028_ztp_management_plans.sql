CREATE TABLE ztp_management_plans (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 baseline_id varchar(32) NOT NULL REFERENCES network_baseline_versions(id),
 content_hash varchar(64) NOT NULL,
 report jsonb NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id,content_hash)
);
CREATE INDEX ztp_management_plans_recent ON ztp_management_plans(project_id,created_at DESC,id DESC);
CREATE TABLE ztp_management_assignments (
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 serial_number varchar(120) NOT NULL,
 hostname varchar(63) NOT NULL,
 management_ip inet NOT NULL,
 gateway inet NOT NULL,
 batch_ref varchar(160) NOT NULL,
 plan_id varchar(32) NOT NULL REFERENCES ztp_management_plans(id),
 PRIMARY KEY(project_id,switch_id),
 UNIQUE(project_id,management_ip),
 UNIQUE(project_id,hostname)
);

CREATE TABLE ztp_environment_checks (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 plan_id varchar(32) NOT NULL REFERENCES ztp_management_plans(id),
 batch_ref varchar(160) NOT NULL,
 boot_channel varchar(8) NOT NULL CHECK(boot_channel IN('239','143')),
 config_hash varchar(64) NOT NULL,
 ready boolean NOT NULL,
 report jsonb NOT NULL,
 checked_by varchar(32) NOT NULL REFERENCES users(id),
 checked_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX ztp_environment_checks_recent ON ztp_environment_checks(project_id,batch_ref,boot_channel,checked_at DESC);
