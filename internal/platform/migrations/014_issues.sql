-- Issue lifecycle (WP-2.7, FR-4.7). Issues unify the six difference sources
-- into one board with source/phase dual markers. The state machine is
-- open -> assigned -> in_repair -> verify -> closed, with reopened allowed
-- from closed; every transition checks authority and the resource version
-- (optimistic lock), and skipping states is rejected. An issue created from
-- an LLDP difference carries the evidence by reference; targeted
-- re-verification in the mock channel re-submits an LLDP snapshot and the
-- judge's resolution closes the loop.
-- Rollback: DROP TABLE issues; DROP TABLE issue_events.
CREATE TABLE issues (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 difference_id varchar(32) REFERENCES lldp_differences(id),
 source varchar NOT NULL CHECK(source IN('topology','g2','g5','g6','portmon','bmc')),
 phase varchar NOT NULL CHECK(phase IN('delivery','post-delivery')),
 category varchar NOT NULL CHECK(category IN('T1','T2','T3','T4','T5','T7','config','other')),
 title varchar(200) NOT NULL,
 evidence jsonb NOT NULL DEFAULT '{}',
 repair_action varchar NOT NULL DEFAULT '' CHECK(repair_action IN('','reseat','replace_cable','replace_module','replace_nic','rma_machine','switch_side','config_fix','bios_update','board_rma')),
 repair_note text NOT NULL DEFAULT '',
 repair_evidence jsonb NOT NULL DEFAULT '{}',
 status varchar NOT NULL DEFAULT 'open' CHECK(status IN('open','assigned','in_repair','verify','closed')),
 assignee varchar(32) REFERENCES users(id),
 version int NOT NULL DEFAULT 1 CHECK(version >= 1),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE INDEX issues_project ON issues(project_id, status, created_at DESC);
CREATE INDEX issues_source ON issues(project_id, source, phase);
CREATE TABLE issue_events (
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL REFERENCES issues(id),
 actor varchar(32) NOT NULL REFERENCES users(id),
 action varchar NOT NULL,
 from_status varchar NOT NULL,
 to_status varchar NOT NULL,
 note text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL
);
CREATE INDEX issue_events_issue ON issue_events(issue_id, created_at);
