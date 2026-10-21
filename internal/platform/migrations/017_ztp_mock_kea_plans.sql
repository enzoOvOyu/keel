-- WP-1.17: durable simulation-only DHCP option staging. This table does not
-- represent a live Kea deployment or physical network readiness evidence.
CREATE TABLE ztp_mock_kea_plans (
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 batch_ref varchar(160) NOT NULL,
 stage_ref varchar(160) NOT NULL,
 staged_config jsonb,
 staged_hash varchar(64),
 active_config jsonb,
 active_hash varchar(64),
 version bigint NOT NULL DEFAULT 1 CHECK(version>=1),
 updated_by varchar(32) NOT NULL REFERENCES users(id),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(project_id,batch_ref,stage_ref),
 CHECK((staged_config IS NULL)=(staged_hash IS NULL)),
 CHECK((active_config IS NULL)=(active_hash IS NULL))
);
