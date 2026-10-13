-- WP-3.3 nettest-svc (FR-11): RoCE connectivity runs, the per-NIC-pair
-- result model, T1-T6 classification and the network-platform handshake.
-- The five layers share one result table: L1 is a per-port self baseline
-- (peer = the port itself), L2-L4 are per-NIC-pair, L5 is per-rail with no
-- peer port. A pair row is only written by the mock ingest channel; the
-- classifier assigns the T category from the observed failure, never from
-- the expectation.
-- Rollback: DROP TABLE nettest_pairs; DROP TABLE nettest_runs;
-- DROP TABLE nettest_platform;
CREATE TABLE nettest_platform (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL UNIQUE REFERENCES projects(id),
 status varchar NOT NULL DEFAULT 'unknown' CHECK(status IN('unknown','ready','degraded','down')),
 detail jsonb NOT NULL DEFAULT '{}',
 refreshed_by varchar(32) NOT NULL REFERENCES users(id),
 refreshed_at timestamptz NOT NULL
);

CREATE TABLE nettest_runs (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 profile varchar NOT NULL DEFAULT 'quick' CHECK(profile IN('quick','full')),
 state varchar NOT NULL DEFAULT 'planned' CHECK(state IN('planned','running','finished','aborted')),
 layers jsonb NOT NULL DEFAULT '[]',
 targets jsonb NOT NULL DEFAULT '[]',
 target_hash varchar(64) NOT NULL,
 batch_size int NOT NULL DEFAULT 8 CHECK(batch_size >= 1),
 params jsonb NOT NULL DEFAULT '{}',
 precheck jsonb NOT NULL DEFAULT '{}',
 coverage jsonb NOT NULL DEFAULT '{}',
 conclusion varchar NOT NULL DEFAULT '' CHECK(conclusion IN('','PASS','FAIL')),
 issue_id varchar(32) NOT NULL DEFAULT '',
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 started_at timestamptz,
 finished_at timestamptz
);
CREATE INDEX nettest_runs_project ON nettest_runs(project_id, created_at DESC);

CREATE TABLE nettest_pairs (
 id varchar(32) PRIMARY KEY,
 run_id varchar(32) NOT NULL REFERENCES nettest_runs(id) ON DELETE CASCADE,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 layer varchar NOT NULL CHECK(layer IN('L1','L2','L3','L4','L5')),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 peer_machine_id varchar(32) NOT NULL REFERENCES machines(id),
 peer_port_id varchar(32) REFERENCES physical_ports(id),
 state varchar NOT NULL DEFAULT 'pending' CHECK(state IN('pending','pass','fail')),
 bandwidth_gbps double precision NOT NULL DEFAULT 0,
 loss_ppm double precision NOT NULL DEFAULT 0,
 pfc_pause bigint NOT NULL DEFAULT 0,
 hotspot bool NOT NULL DEFAULT false,
 classification varchar NOT NULL DEFAULT '',
 detail jsonb NOT NULL DEFAULT '{}',
 measured_at timestamptz
);
CREATE INDEX nettest_pairs_run ON nettest_pairs(run_id, layer, state);
CREATE INDEX nettest_pairs_port ON nettest_pairs(port_id, measured_at DESC);
