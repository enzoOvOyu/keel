-- BOM registers references only. Simulation observations never contain credentials.
CREATE TABLE bom_credential_batches (
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 credential_batch_ref varchar(120) NOT NULL,
 scope varchar(8) NOT NULL CHECK(scope IN ('bmc','switch')),
 rotation_policy varchar(160) NOT NULL,
 vault_path varchar(300) NOT NULL,
 owner varchar(160) NOT NULL,
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,credential_batch_ref)
);
CREATE TABLE ztp_credential_issues (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 session_id varchar(32) NOT NULL REFERENCES ztp_sessions(id),
 switch_id varchar(32) NOT NULL REFERENCES switches(id),
 credential_batch_ref varchar(120) NOT NULL,
 verdict varchar(16) NOT NULL CHECK(verdict IN ('expired','unreachable','unregistered')),
 attempt integer NOT NULL CHECK(attempt>=1),
 occurred_at timestamptz NOT NULL,
 UNIQUE(session_id,attempt)
);
CREATE INDEX ztp_credential_issues_project ON ztp_credential_issues(project_id,occurred_at DESC);
