-- One frozen full scope; each target is still an ordinary BMC Execution.
CREATE TABLE bmc_credential_verifications (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 credential_batch_ref varchar(120) NOT NULL,
 credential_version varchar(120) NOT NULL,
 scope_hash varchar(64) NOT NULL CHECK(scope_hash~'^[a-f0-9]{64}$'),
 scope jsonb NOT NULL CHECK(jsonb_typeof(scope->'targets')='array'),
 target_count integer NOT NULL CHECK(target_count BETWEEN 1 AND 200),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 CHECK(scope->>'ready'='true' AND target_count=(scope->>'target_count')::integer AND target_count=jsonb_array_length(scope->'targets') AND scope->>'project_id'=project_id AND scope->>'credential_batch_ref'=credential_batch_ref AND scope->>'credential_version'=credential_version AND scope->>'scope_hash'=scope_hash),
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '2 hours'),
 FOREIGN KEY(project_id,credential_batch_ref) REFERENCES bom_credential_batches(project_id,credential_batch_ref)
);
CREATE INDEX bmc_credential_verification_history ON bmc_credential_verifications(project_id,credential_batch_ref,created_at DESC,id DESC);
CREATE TABLE bmc_credential_verification_targets (
 verification_id varchar(32) NOT NULL REFERENCES bmc_credential_verifications(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 snapshot jsonb NOT NULL,
 execution_id varchar(32) NOT NULL UNIQUE REFERENCES exec_executions(id),
 collection_id varchar(32) NOT NULL UNIQUE REFERENCES bmc_collection_requests(id),
 PRIMARY KEY(verification_id,asset_id)
);
CREATE TABLE bmc_credential_verification_results (
 id varchar(32) PRIMARY KEY,
 verification_id varchar(32) NOT NULL UNIQUE REFERENCES bmc_credential_verifications(id),
 outcome varchar(12) NOT NULL CHECK(outcome IN('passed','simulated','failed','stale','expired')),
 code varchar(120) NOT NULL,
 targets jsonb NOT NULL CHECK(jsonb_typeof(targets)='array'),
 finished_at timestamptz NOT NULL
);
CREATE TRIGGER bmc_credential_verification_immutable BEFORE UPDATE OR DELETE ON bmc_credential_verifications FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER bmc_credential_target_immutable BEFORE UPDATE OR DELETE ON bmc_credential_verification_targets FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER bmc_credential_result_immutable BEFORE UPDATE OR DELETE ON bmc_credential_verification_results FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();

-- This view defines membership once, without accepting a browser target subset.
CREATE VIEW bmc_current_credential_members AS
SELECT a.project_id,a.id AS asset_id,a.machine_id,a.version AS asset_version,m.serial_number AS serial,b.package_id,b.batch_ref
FROM project_assets a JOIN machines m ON m.id=a.machine_id JOIN bom_items item ON item.item_uid=a.machine_id AND item.status<>'retired'
JOIN LATERAL (SELECT v.package_id,v.row_snapshot#>>'{values,credential_batch_ref}' AS batch_ref FROM bom_item_revisions v JOIN bom_import_packages p ON p.id=v.package_id AND p.project_id=v.project_id AND p.state='committed' WHERE v.project_id=a.project_id AND v.item_uid=a.machine_id ORDER BY p.committed_at DESC,p.id DESC LIMIT 1) b ON true;

CREATE FUNCTION keel_bmc_verification_scope_current(request_id varchar) RETURNS boolean LANGUAGE sql STABLE SET search_path=public,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM bmc_credential_verifications q JOIN device_credential_heads h ON h.project_id=q.project_id AND h.credential_batch_ref=q.credential_batch_ref AND h.scope='bmc' AND h.credential_version=q.credential_version JOIN device_credential_versions v ON v.project_id=h.project_id AND v.credential_batch_ref=h.credential_batch_ref AND v.scope=h.scope AND v.credential_version=h.credential_version JOIN projects p ON p.id=q.project_id JOIN users u ON u.id=q.created_by
 WHERE q.id=request_id AND v.expires_at>statement_timestamp() AND NOT EXISTS(SELECT 1 FROM device_credential_revocations revoked WHERE (revoked.project_id,revoked.credential_batch_ref,revoked.scope,revoked.credential_version)=(h.project_id,h.credential_batch_ref,h.scope,h.credential_version))
 AND p.state NOT IN('archived','delivered') AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships member WHERE member.project_id=q.project_id AND member.user_id=u.id AND member.role IN('Admin','DE','DL','SRE')))
 AND (SELECT count(*) FROM bmc_credential_verification_targets t WHERE t.verification_id=q.id)=q.target_count
 AND (SELECT count(*) FROM bmc_current_credential_members member WHERE member.project_id=q.project_id AND member.batch_ref=q.credential_batch_ref)=q.target_count
 AND NOT EXISTS(SELECT 1 FROM bmc_credential_verification_targets t WHERE t.verification_id=q.id AND NOT EXISTS(SELECT 1 FROM bmc_current_credential_members member WHERE member.project_id=q.project_id AND member.batch_ref=q.credential_batch_ref AND member.asset_id=t.asset_id AND member.machine_id=t.machine_id AND member.asset_version=(t.snapshot->>'asset_version')::bigint AND member.serial=t.snapshot->>'serial' AND member.package_id=t.snapshot->>'bom_package_id')));
$$;
CREATE FUNCTION keel_bmc_verification_evidence_current(request_id varchar,real_only boolean) RETURNS boolean LANGUAGE sql STABLE SET search_path=public,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM bmc_credential_verifications q WHERE q.id=request_id AND keel_bmc_verification_scope_current(q.id)
 AND NOT EXISTS(SELECT 1 FROM bmc_credential_verification_targets t WHERE t.verification_id=q.id AND NOT EXISTS(
 SELECT 1 FROM bmc_collection_records r JOIN bmc_collection_requests request ON request.id=r.id JOIN exec_executions x ON x.id=t.execution_id JOIN exec_task_attempts a ON a.execution_id=x.id AND a.fencing_token=r.fencing_token JOIN exec_runner_receipts receipt ON receipt.attempt_id=a.id AND receipt.command_id=a.command_id AND receipt.fencing_token=a.fencing_token
 WHERE r.id=t.collection_id AND r.execution_id=t.execution_id AND r.asset_id=t.asset_id AND r.machine_id=t.machine_id AND x.state IN('success','failed') AND x.desired_state='running' AND a.state IN('success','failed') AND receipt.result=a.state AND receipt.payload#>>'{output,stopped}'='true' AND receipt.payload#>>'{output,evidence_hash}'=r.content_hash
 AND r.state<>'failed' AND r.origin=t.snapshot->>'origin' AND (NOT real_only OR r.origin='real') AND r.report->>'identity_matched'='true' AND r.report#>>'{target,credential_version}'=q.credential_version AND r.report#>>'{target,adapter_binding_hash}'=t.snapshot->>'adapter_binding_hash' AND r.collected_at>=q.created_at AND r.collected_at<=q.expires_at
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(r.report->'failures') f WHERE f->>'class'='authentication_failed')
 AND EXISTS(SELECT 1 FROM bmc_collection_resources resource WHERE resource.collection_id=r.id AND (resource.resource_type LIKE '#ComputerSystem.%' OR resource.resource_path='/ipmi/v2/inventory/fru-0')))));
$$;
ALTER TABLE device_credential_heads ADD COLUMN verification_receipt_id varchar(32) REFERENCES bmc_credential_verification_results(id);
ALTER TABLE device_credential_heads ADD CONSTRAINT device_credential_verification_reference CHECK((verification_state='verified')=(verification_receipt_id IS NOT NULL));
CREATE FUNCTION keel_bmc_credential_verified(project varchar,batch varchar,version varchar) RETURNS boolean LANGUAGE sql STABLE SET search_path=public,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM device_credential_heads h JOIN bmc_credential_verification_results proof ON proof.id=h.verification_receipt_id JOIN bmc_credential_verifications q ON q.id=proof.verification_id WHERE h.project_id=project AND h.credential_batch_ref=batch AND h.scope='bmc' AND h.credential_version=version AND q.project_id=h.project_id AND q.credential_batch_ref=h.credential_batch_ref AND q.credential_version=h.credential_version AND proof.outcome='passed' AND proof.finished_at<=q.expires_at AND keel_bmc_verification_evidence_current(q.id,true));
$$;
-- Application role may assign only a proof reference, never replace ciphertext,
-- current versions or expiry. The trigger derives the cached status itself.
CREATE FUNCTION keel_bmc_credential_verification_assignment() RETURNS trigger LANGUAGE plpgsql SET search_path=public,pg_temp AS $$
BEGIN
 IF NEW.verification_receipt_id IS NULL THEN NEW.verification_state:='pending';
 ELSIF NEW.scope<>'bmc' OR NOT EXISTS(SELECT 1 FROM bmc_credential_verification_results proof JOIN bmc_credential_verifications q ON q.id=proof.verification_id WHERE proof.id=NEW.verification_receipt_id AND proof.outcome='passed' AND q.project_id=NEW.project_id AND q.credential_batch_ref=NEW.credential_batch_ref AND q.credential_version=NEW.credential_version AND proof.finished_at<=q.expires_at AND keel_bmc_verification_evidence_current(q.id,true)) THEN RAISE EXCEPTION 'current full real credential verification proof required';
 ELSE NEW.verification_state:='verified'; END IF;
 NEW.state_version:=OLD.state_version+1;NEW.updated_at:=statement_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER bmc_credential_verification_assignment BEFORE UPDATE OF verification_receipt_id ON device_credential_heads FOR EACH ROW EXECUTE FUNCTION keel_bmc_credential_verification_assignment();
CREATE FUNCTION keel_bmc_credential_target_scope() RETURNS trigger LANGUAGE plpgsql SET search_path=public,pg_temp AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM bmc_credential_verifications q JOIN bmc_collection_requests request ON request.id=NEW.collection_id JOIN project_assets asset ON asset.id=NEW.asset_id JOIN exec_executions execution ON execution.id=NEW.execution_id AND execution.parameters->>'bmc_credential_verification_id'=NEW.verification_id WHERE q.id=NEW.verification_id AND asset.project_id=q.project_id AND asset.machine_id=NEW.machine_id AND request.project_id=q.project_id AND request.asset_id=NEW.asset_id AND request.machine_id=NEW.machine_id AND request.execution_id=NEW.execution_id AND request.credential_batch_ref=q.credential_batch_ref AND request.credential_version=q.credential_version AND request.target->>'adapter_binding_hash'=NEW.snapshot->>'adapter_binding_hash' AND request.target->>'bom_package_id'=NEW.snapshot->>'bom_package_id' AND request.target->>'serial'=NEW.snapshot->>'serial' AND request.asset_version=(NEW.snapshot->>'asset_version')::bigint AND EXISTS(SELECT 1 FROM jsonb_array_elements(q.scope->'targets') expected WHERE expected=NEW.snapshot)) THEN RAISE EXCEPTION 'credential verification target must match its full scope and execution'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bmc_credential_target_scope BEFORE INSERT ON bmc_credential_verification_targets FOR EACH ROW EXECUTE FUNCTION keel_bmc_credential_target_scope();
CREATE FUNCTION keel_bmc_credential_result_scope() RETURNS trigger LANGUAGE plpgsql SET search_path=public,pg_temp AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM bmc_credential_verifications q WHERE q.id=NEW.verification_id AND jsonb_array_length(NEW.targets)=q.target_count AND NOT EXISTS(SELECT 1 FROM bmc_credential_verification_targets t WHERE t.verification_id=q.id AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(NEW.targets) progress WHERE progress->>'asset_id'=t.asset_id AND progress->>'machine_id'=t.machine_id AND progress->>'execution_id'=t.execution_id AND progress->>'collection_id'=t.collection_id))) THEN RAISE EXCEPTION 'result must describe every frozen verification member'; END IF;
 IF NEW.outcome IN('passed','simulated') AND NOT EXISTS(SELECT 1 FROM bmc_credential_verifications q WHERE q.id=NEW.verification_id AND NEW.finished_at>=q.created_at AND NEW.finished_at<=q.expires_at AND NEW.finished_at<=statement_timestamp()) THEN RAISE EXCEPTION 'verification must finish inside its frozen window'; END IF;
 IF NEW.outcome='passed' AND NOT keel_bmc_verification_evidence_current(NEW.verification_id,true) THEN RAISE EXCEPTION 'real full-batch evidence required'; END IF;
 IF NEW.outcome='simulated' AND NOT keel_bmc_verification_evidence_current(NEW.verification_id,false) THEN RAISE EXCEPTION 'simulated full-batch evidence required'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER bmc_credential_result_scope BEFORE INSERT ON bmc_credential_verification_results FOR EACH ROW EXECUTE FUNCTION keel_bmc_credential_result_scope();
