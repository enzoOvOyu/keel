-- WP-3.4 driver-svc (FR-5.6, F-5.3): the driver and dependency upgrade
-- chain. A plan selects component targets from approved media (media://
-- URIs, F-1.8), prechecks the current->target matrix against the component
-- capability matrix, then runs canary -> batched rolling -> read-back.
-- Failures recover by declared component capability: a verified rollback
-- path restores the pre-upgrade backup; firmware is never assumed
-- rollback-able and routes to manual handling. Distribution is accounted
-- per target: a cache hit skips the transfer (F-1.7), a checksum mismatch
-- retries a bounded number of times and then isolates the target (F-1.6).
-- Rollback: DROP TABLE driver_transfer_log;
-- DROP TABLE driver_upgrade_targets; DROP TABLE driver_upgrade_plans;
-- DROP TABLE driver_components;

-- The capability matrix is platform knowledge, not per-project data: which
-- components exist, what an upgrade of them requires, and what recovery
-- looks like. rollback='rollback_verified' means a pre-upgrade backup plus a
-- verified restore path exists; 'manual_only' means recovery needs a human
-- (firmware lives here by policy); 'none' means no recovery is claimed.
CREATE TABLE driver_components (
 key varchar(40) PRIMARY KEY,
 display_name varchar(120) NOT NULL,
 media_type varchar NOT NULL CHECK(media_type IN('driver','firmware','os','nccl','tool','config')),
 rollback varchar NOT NULL DEFAULT 'none' CHECK(rollback IN('rollback_verified','manual_only','none')),
 backup_required bool NOT NULL DEFAULT false,
 companion_rule jsonb NOT NULL DEFAULT '{}',
 notes text NOT NULL DEFAULT ''
);

INSERT INTO driver_components(key,display_name,media_type,rollback,backup_required,companion_rule,notes) VALUES
 ('gpu_driver','GPU 驱动程序','driver','rollback_verified',true,'{"cuda_at_least":"13.0"}','R580+ 与 CUDA 13.x 为已验证组合；升级前须备份当前驱动'),
 ('cuda','CUDA Toolkit','driver','rollback_verified',false,'{}','与 GPU 驱动同栈升级时按组合矩阵校验'),
 ('ofed','OFED 驱动','driver','rollback_verified',true,'{}','升级前须备份当前 OFED 配置'),
 ('doca','DOCA 平台','driver','rollback_verified',false,'{}','依赖 OFED 版本，组合校验见 companion_rule'),
 ('cx8_firmware','CX8 网卡固件','firmware','manual_only',true,'{}','固件不假定可回滚：失败转人工处理，禁止自动重刷'),
 ('kernel','操作系统内核','os','manual_only',true,'{}','内核升级失败转人工，凭备份介质恢复');

CREATE TABLE driver_upgrade_plans (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 name varchar(120) NOT NULL,
 state varchar NOT NULL DEFAULT 'draft' CHECK(state IN('draft','prechecked','canary_running','rolling','completed','rolled_back','manual_required','aborted')),
 components jsonb NOT NULL DEFAULT '{}',
 machines jsonb NOT NULL DEFAULT '[]',
 baseline jsonb NOT NULL DEFAULT '{}',
 baseline_overrides jsonb NOT NULL DEFAULT '{}',
 canary_machine_id varchar(32) NOT NULL,
 batch_size int NOT NULL DEFAULT 4 CHECK(batch_size >= 1 AND batch_size <= 64),
 backup_plan text NOT NULL DEFAULT '',
 precheck jsonb NOT NULL DEFAULT '{}',
 conclusion varchar NOT NULL DEFAULT '' CHECK(conclusion IN('','PASS','FAIL')),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 started_at timestamptz,
 finished_at timestamptz
);
CREATE INDEX driver_plans_project ON driver_upgrade_plans(project_id, created_at DESC);

CREATE TABLE driver_upgrade_targets (
 id varchar(32) PRIMARY KEY,
 plan_id varchar(32) NOT NULL REFERENCES driver_upgrade_plans(id) ON DELETE CASCADE,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 role varchar NOT NULL DEFAULT 'batch' CHECK(role IN('canary','batch')),
 current_versions jsonb NOT NULL DEFAULT '{}',
 target_versions jsonb NOT NULL DEFAULT '{}',
 state varchar NOT NULL DEFAULT 'pending' CHECK(state IN('pending','precheck_blocked','distributing','installing','verifying','verified','failed','rolled_back','manual','reconcile')),
 cache_hit bool NOT NULL DEFAULT false,
 bytes_transferred bigint NOT NULL DEFAULT 0,
 readback jsonb NOT NULL DEFAULT '{}',
 failure_class varchar NOT NULL DEFAULT '',
 attempt int NOT NULL DEFAULT 0,
 reconcile jsonb NOT NULL DEFAULT '{}',
 evidence jsonb NOT NULL DEFAULT '{}',
 updated_at timestamptz NOT NULL,
 UNIQUE(plan_id, machine_id)
);
CREATE INDEX driver_targets_plan ON driver_upgrade_targets(plan_id, role, machine_id);

CREATE TABLE driver_transfer_log (
 id varchar(32) PRIMARY KEY,
 plan_id varchar(32) NOT NULL REFERENCES driver_upgrade_plans(id) ON DELETE CASCADE,
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 component varchar(40) NOT NULL,
 uri varchar(200) NOT NULL,
 sha256 varchar(64) NOT NULL,
 size bigint NOT NULL,
 cache_hit bool NOT NULL,
 bytes bigint NOT NULL,
 checksum varchar NOT NULL DEFAULT '' CHECK(checksum IN('','match','mismatch')),
 retries int NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL
);
CREATE INDEX driver_transfer_plan ON driver_transfer_log(plan_id, machine_id, created_at);
