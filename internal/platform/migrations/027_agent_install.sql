-- WP-3.5 agent installer (FR-5.7, F-5.4): the keel-agent binary is itself a
-- media:// artifact (media://agent/keel-agent@<version>). Batch installation
-- runs only against delivery-state machines with a valid G3+ evidence base:
-- the project must have a published mapping revision for the machine (the G3
-- gate contract, same source of truth as batch config). Each installation
-- materializes a per-machine row with an enrollment credential lifecycle:
-- a one-time short-lived enrollment token is issued bound to project+machine,
-- the machine presents it (with its serial + fingerprint), the server verifies
-- device ownership and issues a short-lived device mTLS certificate. Token
-- replay is refused; certificate rotation and revocation are audited. A
-- machine cannot self-register on a bare SN report alone.
-- Rollback: DROP TABLE agent_credential_events; DROP TABLE
-- agent_installation_targets; DROP TABLE agent_install_plans; then re-add the
-- original media_items type CHECK without 'agent'.

-- The agent binary is media (FR-5.7): widen the media type CHECK.
ALTER TABLE media_items DROP CONSTRAINT media_items_type_check;
ALTER TABLE media_items ADD CONSTRAINT media_items_type_check
 CHECK(type IN('driver','firmware','os','nccl','tool','config','agent'));

CREATE TABLE agent_install_plans (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 name varchar(120) NOT NULL,
 state varchar NOT NULL DEFAULT 'draft' CHECK(state IN('draft','running','completed','failed','aborted')),
 -- resolved media reference: {media_id, name, version, arch, sha256, uri}
 media jsonb NOT NULL DEFAULT '{}',
 -- requested machine ids, frozen at creation
 machines jsonb NOT NULL DEFAULT '[]',
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 finished_at timestamptz
);
CREATE INDEX agent_plans_project ON agent_install_plans(project_id, created_at DESC);

CREATE TABLE agent_installation_targets (
 id varchar(32) PRIMARY KEY,
 plan_id varchar(32) NOT NULL REFERENCES agent_install_plans(id) ON DELETE CASCADE,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL,
 hostname text NOT NULL DEFAULT '',
 -- pending -> installing -> installed (systemd registered, service started,
 -- heartbeat reported back) / failed / blocked (G3+ admission refused)
 state varchar NOT NULL DEFAULT 'pending' CHECK(state IN('pending','installing','installed','failed','blocked')),
 -- human-readable reason for blocked/failed rows
 reason text NOT NULL DEFAULT '',
 -- the resolved media uri proving media://agent distribution (F-5.4)
 media_uri text NOT NULL DEFAULT '',
 media_sha256 varchar(64) NOT NULL DEFAULT '',
 -- distribution accounting: cache hit skips the transfer (F-1.7)
 cache_hit bool NOT NULL DEFAULT false,
 bytes_transferred bigint NOT NULL DEFAULT 0,
 -- enrollment + credential lifecycle trace
 enrollment_token varchar(64) NOT NULL DEFAULT '',
 credential_id varchar(32) NOT NULL DEFAULT '',
 credential_fingerprint varchar(160) NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL
);
CREATE INDEX agent_targets_plan ON agent_installation_targets(plan_id);
CREATE INDEX agent_targets_machine ON agent_installation_targets(project_id, machine_id);

-- Credential lifecycle events (issue/rotate/revoke + token replay refusals).
-- The mTLS material itself is out of scope for the control-plane store: this
-- table records the auditable lifecycle of the credential identities only.
CREATE TABLE agent_credential_events (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL,
 credential_id varchar(32) NOT NULL,
 -- issue | rotate | revoke | token_replay_refused
 action varchar NOT NULL CHECK(action IN('issue','rotate','revoke','token_replay_refused')),
 detail jsonb NOT NULL DEFAULT '{}',
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL
);
CREATE INDEX agent_cred_events_machine ON agent_credential_events(project_id, machine_id, created_at DESC);
