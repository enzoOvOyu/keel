-- ConfigSet engine (WP-3.1, FR-5.1). A ConfigSet is data: keys + schema +
-- scope + a Go template whose version-class variables reference parameter
-- catalog placeholders resolved from the frozen parameter snapshot at
-- apply time. Changes flow through versioned approval: a draft becomes an
-- approved immutable revision (single publish path, aligned with the
-- model-version semantics); rendering previews per machine diff against
-- the target's current applied state.
-- Rollback: DROP TABLE config_set_applies; DROP TABLE config_sets;
CREATE TABLE config_sets (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 name varchar(120) NOT NULL,
 version int NOT NULL CHECK(version >= 1),
 status varchar NOT NULL DEFAULT 'draft' CHECK(status IN('draft','approved','retired')),
 scope varchar NOT NULL DEFAULT 'machine' CHECK(scope IN('machine','model','project')),
 model_name varchar(120) NOT NULL DEFAULT '',
 template text NOT NULL,
 schema jsonb NOT NULL DEFAULT '{}',
 keys jsonb NOT NULL DEFAULT '{}',
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 approved_by varchar(32) REFERENCES users(id),
 approved_at timestamptz,
 UNIQUE(project_id, name, version)
);
CREATE INDEX config_sets_project ON config_sets(project_id, name, version DESC);
CREATE TABLE config_set_applies (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 config_set_id varchar(32) NOT NULL REFERENCES config_sets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 rendered text NOT NULL,
 diff text NOT NULL DEFAULT '',
 applied_by varchar(32) NOT NULL REFERENCES users(id),
 applied_at timestamptz NOT NULL
);
CREATE INDEX config_set_applies_machine ON config_set_applies(machine_id, applied_at DESC);
