-- Triggers are durable admission hints. All device work still uses Execution.
CREATE TABLE bmc_inventory_events (
 sequence bigserial PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 event_type varchar(40) NOT NULL CHECK(event_type IN('bom.committed','credential.batch.rotated','asset.provisioned')),
 source_ref varchar(300) NOT NULL,
 machine_id varchar(32) REFERENCES machines(id),
 credential_batch_ref varchar(120),
 occurred_at timestamptz NOT NULL,
 UNIQUE(project_id,event_type,source_ref)
);
CREATE INDEX bmc_inventory_event_scope ON bmc_inventory_events(project_id,event_type,sequence);
CREATE TRIGGER bmc_inventory_event_immutable BEFORE UPDATE OR DELETE ON bmc_inventory_events FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TABLE bmc_collection_policies (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 level varchar(2) NOT NULL CHECK(level IN('L1','L2','L3')),
 trigger_kind varchar(8) NOT NULL CHECK(trigger_kind IN('cron','event')),
 cron_expression varchar(160) NOT NULL DEFAULT '',
 event_type varchar(40) NOT NULL DEFAULT '',
 enabled boolean NOT NULL,
 state_version bigint NOT NULL CHECK(state_version>0),
 max_retries smallint NOT NULL CHECK(max_retries BETWEEN 0 AND 3),
 retry_delay_seconds integer NOT NULL CHECK(retry_delay_seconds BETWEEN 30 AND 3600),
 next_due timestamptz,
 last_event_sequence bigint NOT NULL DEFAULT 0 CHECK(last_event_sequence>=0),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK((trigger_kind='cron' AND length(cron_expression)>0 AND event_type='' AND (NOT enabled OR next_due IS NOT NULL)) OR (trigger_kind='event' AND next_due IS NULL AND cron_expression='' AND event_type IN('bom.committed','credential.batch.rotated','asset.provisioned')))
);
CREATE INDEX bmc_policy_asset ON bmc_collection_policies(project_id,asset_id,id);
CREATE INDEX bmc_policy_due ON bmc_collection_policies(next_due,id) WHERE enabled AND trigger_kind='cron';
CREATE INDEX bmc_policy_events ON bmc_collection_policies(project_id,event_type,last_event_sequence) WHERE enabled AND trigger_kind='event';
CREATE TABLE bmc_policy_runs (
 id varchar(32) PRIMARY KEY,
 policy_id varchar(32) NOT NULL REFERENCES bmc_collection_policies(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 policy_version bigint NOT NULL,
 policy_snapshot jsonb NOT NULL,
 trigger_key varchar(200) NOT NULL,
 trigger_kind varchar(8) NOT NULL CHECK(trigger_kind IN('cron','event','retry')),
 source_ref varchar(300) NOT NULL,
 parent_run_id varchar(32) UNIQUE REFERENCES bmc_policy_runs(id),
 attempt_no smallint NOT NULL CHECK(attempt_no BETWEEN 0 AND 3),
 execution_id varchar(32) UNIQUE REFERENCES exec_executions(id),
 state varchar(10) NOT NULL CHECK(state IN('admitted','blocked')),
 code varchar(120) NOT NULL,
 scheduled_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL,
 UNIQUE(policy_id,trigger_key),
 CHECK((state='admitted' AND execution_id IS NOT NULL) OR (state='blocked' AND execution_id IS NULL))
);
CREATE INDEX bmc_policy_retry_candidates ON bmc_policy_runs(policy_id,policy_version,attempt_no) WHERE state='admitted';
CREATE INDEX bmc_policy_run_history ON bmc_policy_runs(policy_id,created_at DESC,id DESC);
CREATE TRIGGER bmc_policy_run_immutable BEFORE UPDATE OR DELETE ON bmc_policy_runs FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_bmc_policy_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM project_assets a WHERE a.id=NEW.asset_id AND a.project_id=NEW.project_id) THEN RAISE EXCEPTION 'inventory policy asset scope mismatch'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.id,NEW.project_id,NEW.asset_id,NEW.created_by,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.project_id,OLD.asset_id,OLD.created_by,OLD.created_at) THEN RAISE EXCEPTION 'inventory policy ownership is immutable'; END IF;
  IF NEW.state_version<OLD.state_version OR NEW.state_version>OLD.state_version+1 OR NEW.last_event_sequence<OLD.last_event_sequence THEN RAISE EXCEPTION 'inventory policy version or cursor regressed'; END IF;
  IF (NEW.level,NEW.trigger_kind,NEW.cron_expression,NEW.event_type,NEW.enabled,NEW.max_retries,NEW.retry_delay_seconds) IS DISTINCT FROM (OLD.level,OLD.trigger_kind,OLD.cron_expression,OLD.event_type,OLD.enabled,OLD.max_retries,OLD.retry_delay_seconds) AND NEW.state_version<>OLD.state_version+1 THEN RAISE EXCEPTION 'inventory policy change requires next version'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bmc_policy_scope BEFORE INSERT OR UPDATE ON bmc_collection_policies FOR EACH ROW EXECUTE FUNCTION keel_bmc_policy_scope();
CREATE FUNCTION keel_bmc_policy_run_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM bmc_collection_policies p WHERE p.id=NEW.policy_id AND p.project_id=NEW.project_id AND NEW.policy_version>0 AND NEW.policy_version<=p.state_version AND NEW.policy_snapshot->>'id'=p.id AND NEW.policy_snapshot->>'project_id'=p.project_id AND NEW.policy_snapshot->>'asset_id'=p.asset_id AND (NEW.policy_snapshot->>'state_version')::bigint=NEW.policy_version) THEN RAISE EXCEPTION 'inventory run policy scope mismatch'; END IF;
 IF NEW.execution_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM exec_executions x JOIN bmc_collection_requests q ON q.execution_id=x.id JOIN bmc_collection_policies p ON p.id=NEW.policy_id WHERE x.id=NEW.execution_id AND x.mode='bmc' AND x.project_id=NEW.project_id AND q.asset_id=p.asset_id AND x.parameters#>>'{bmc_trigger,run_id}'=NEW.id AND x.parameters#>>'{bmc_trigger,policy_id}'=NEW.policy_id AND (x.parameters#>>'{bmc_trigger,policy_version}')::bigint=NEW.policy_version) THEN RAISE EXCEPTION 'inventory run execution scope mismatch'; END IF;
 IF NEW.trigger_kind='retry' THEN
  IF NEW.parent_run_id IS NULL OR NEW.attempt_no>coalesce((NEW.policy_snapshot->>'max_retries')::integer,0) OR NOT EXISTS(SELECT 1 FROM bmc_policy_runs r WHERE r.id=NEW.parent_run_id AND r.policy_id=NEW.policy_id AND r.project_id=NEW.project_id AND r.policy_version=NEW.policy_version AND r.state='admitted' AND NEW.attempt_no=r.attempt_no+1) THEN RAISE EXCEPTION 'inventory retry parent mismatch'; END IF;
 ELSIF NEW.parent_run_id IS NOT NULL OR NEW.attempt_no<>0 THEN RAISE EXCEPTION 'initial inventory trigger cannot be a retry'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bmc_policy_run_scope BEFORE INSERT ON bmc_policy_runs FOR EACH ROW EXECUTE FUNCTION keel_bmc_policy_run_scope();
