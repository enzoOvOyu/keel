-- Monitoring data plane (WP-2.10, FR-13 mock channel). Policies carry the
-- stage-based collection tiers (M1-M4 per the FR-13.2 table); ingest accepts
-- changed-items-only samples (bandwidth O(changes)); the flapping alert
-- rule (>=3 link-down events within 10 minutes) derives from the sample
-- stream; budgets cap steady-state and burst rates per machine.
-- Rollback: DROP TABLE mon_alerts; DROP TABLE mon_samples; DROP TABLE mon_policies.
CREATE TABLE mon_policies (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 stage varchar NOT NULL CHECK(stage IN('M1','M2','M3','M4','maintenance')),
 d1_d2_interval_seconds int NOT NULL,
 d3_interval_seconds int NOT NULL,
 d4_interval_seconds int NOT NULL,
 d5_interval_seconds int NOT NULL,
 burst_multiplier int NOT NULL DEFAULT 1 CHECK(burst_multiplier >= 1),
 steady_budget_kbps int NOT NULL DEFAULT 50 CHECK(steady_budget_kbps > 0),
 burst_budget_kbps int NOT NULL DEFAULT 200 CHECK(burst_budget_kbps > 0),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id, stage)
);
CREATE TABLE mon_samples (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 port varchar(64) NOT NULL DEFAULT '',
 metric varchar NOT NULL,
 value jsonb NOT NULL,
 source varchar NOT NULL DEFAULT 'agent',
 ingested_at timestamptz NOT NULL
);
CREATE INDEX mon_samples_stream ON mon_samples(project_id, machine_id, metric, ingested_at DESC);
CREATE TABLE mon_alerts (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 port varchar(64) NOT NULL DEFAULT '',
 rule varchar NOT NULL,
 detail jsonb NOT NULL DEFAULT '{}',
 opened_at timestamptz NOT NULL,
 closed_at timestamptz
);
CREATE INDEX mon_alerts_project ON mon_alerts(project_id, opened_at DESC);
