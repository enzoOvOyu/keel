-- Redfish collection records (WP-2.1, FR-3). Each record binds one machine
-- with a level (L1/L2/L3), stores the raw snapshot, a structured view, the
-- collection revision and a freshness computed at read time from
-- collected_at versus the record's freshness budget. Failures are
-- classified and retryable and never overwrite the last good snapshot;
-- history is retained (the current record is simply the newest per
-- machine+level). Credential rotation (vault batches) stays planned.
-- Rollback: DROP TABLE collection_records.
CREATE TABLE collection_records (
 id varchar(32) PRIMARY KEY,
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 level varchar NOT NULL CHECK(level IN('L1','L2','L3')),
 status varchar NOT NULL CHECK(status IN('success','failed')),
 failure_class varchar NOT NULL DEFAULT '' CHECK(failure_class IN('','auth_failed','timeout','version_unsupported','partial','credential_expired')),
 raw jsonb NOT NULL DEFAULT '{}',
 structured jsonb NOT NULL DEFAULT '{}',
 collection_revision int NOT NULL DEFAULT 0 CHECK(collection_revision >= 0),
 freshness_max_age_seconds int NOT NULL DEFAULT 86400 CHECK(freshness_max_age_seconds > 0),
 collected_by varchar(32) NOT NULL REFERENCES users(id),
 collected_at timestamptz NOT NULL
);
CREATE INDEX collection_records_machine ON collection_records(machine_id, level, collected_at DESC);
CREATE INDEX collection_records_project ON collection_records(project_id, collected_at DESC);
