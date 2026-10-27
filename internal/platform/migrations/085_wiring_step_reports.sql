-- Immutable StepReport pipeline. This increment publishes only Step 1; later
-- steps require domain-owned retained execution receipts, never uploaded PASS.
CREATE TABLE validation_wiring_batches(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 handshake_id varchar(32) NOT NULL UNIQUE REFERENCES validation_wiring_handshakes(id),
 facts_hash varchar(64) NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL,
 UNIQUE(id,project_id)
);
CREATE TABLE validation_wiring_batch_targets(
 batch_id varchar(32) NOT NULL,
 project_id varchar(32) NOT NULL,
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 topology_id varchar(32) NOT NULL REFERENCES pci_topology_snapshots(id),
 expectation jsonb NOT NULL,
 relations jsonb NOT NULL,
 PRIMARY KEY(batch_id,asset_id),
 FOREIGN KEY(batch_id,project_id) REFERENCES validation_wiring_batches(id,project_id),
 CHECK((expectation->>'version'='keel.wiring/v1' AND expectation->>'project_id'=project_id AND expectation->>'asset_id'=asset_id AND expectation->>'machine_id'=machine_id AND relations->>'snapshot_id'=topology_id) IS TRUE)
);
CREATE TABLE validation_wiring_step_reports(
 id varchar(32) PRIMARY KEY,
 batch_id varchar(32) NOT NULL,
 asset_id varchar(32) NOT NULL,
 step smallint NOT NULL CHECK(step BETWEEN 1 AND 4),
 version bigint NOT NULL CHECK(version>0),
 parent_id varchar(32) REFERENCES validation_wiring_step_reports(id),
 source_hash varchar(64) NOT NULL CHECK(source_hash ~ '^[0-9a-f]{64}$'),
 verdict varchar(8) NOT NULL CHECK(verdict IN('PASS','FAIL','BLOCKED')),
 detail jsonb NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 FOREIGN KEY(batch_id,asset_id) REFERENCES validation_wiring_batch_targets(batch_id,asset_id),
 UNIQUE(batch_id,asset_id,step,version),
 CHECK((step=1 AND parent_id IS NULL) OR (step>1 AND parent_id IS NOT NULL)),
 CHECK((detail->>'version'='keel.wiring-step/v1' AND (detail->>'step')::int=step AND detail->>'verdict'=verdict) IS TRUE)
);
CREATE INDEX validation_wiring_batch_history ON validation_wiring_batches(project_id,created_at DESC,id DESC);
CREATE INDEX validation_wiring_step_history ON validation_wiring_step_reports(batch_id,asset_id,step,version DESC);
CREATE TRIGGER validation_wiring_batch_immutable BEFORE UPDATE OR DELETE ON validation_wiring_batches FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER validation_wiring_batch_target_immutable BEFORE UPDATE OR DELETE ON validation_wiring_batch_targets FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER validation_wiring_step_immutable BEFORE UPDATE OR DELETE ON validation_wiring_step_reports FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_wiring_step_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE host jsonb; frozen_hash text; expected_ports jsonb;
BEGIN
 IF TG_TABLE_NAME='validation_wiring_batches' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_wiring_handshakes h WHERE h.id=NEW.handshake_id AND h.project_id=NEW.project_id AND h.facts_hash=NEW.facts_hash AND h.valid_until>statement_timestamp() AND NEW.created_at>=h.created_at AND NEW.created_at<h.valid_until) THEN RAISE EXCEPTION 'batch must consume a current exact project handshake'; END IF;
 ELSIF TG_TABLE_NAME='validation_wiring_batch_targets' THEN
  SELECT target,b.facts_hash INTO host,frozen_hash FROM validation_wiring_batches b JOIN validation_wiring_handshakes h ON h.id=b.handshake_id CROSS JOIN LATERAL jsonb_array_elements(h.facts->'targets') target WHERE b.id=NEW.batch_id AND b.project_id=NEW.project_id AND target->>'asset_id'=NEW.asset_id;
  IF host IS NULL OR host->>'machine_id'<>NEW.machine_id OR host->>'topology_id'<>NEW.topology_id THEN RAISE EXCEPTION 'batch target must match the frozen selected host'; END IF;
  SELECT jsonb_agg(jsonb_build_object('physical_port_id',p->>'physical_port_id','component_id',p->>'component_id','port_index',p->>'port_index','interface',p->>'interface','permanent_mac',p->>'permanent_mac','bdf',p->>'bdf','rail',p->>'rail','cable_id',p->>'cable_id','switch_id',p->>'switch_id','switch_serial',p->>'switch_serial','switch_chassis_ids',p->'switch_chassis_ids','switch_port',p->>'switch_port') ORDER BY ordinal) INTO expected_ports FROM jsonb_array_elements(host->'ports') WITH ORDINALITY AS selected(p,ordinal);
  IF NEW.expectation IS DISTINCT FROM jsonb_build_object('version','keel.wiring/v1','project_id',NEW.project_id,'asset_id',NEW.asset_id,'machine_id',NEW.machine_id,'serial',host->>'serial','hostname',host->>'hostname','scope_hash',frozen_hash,'ports',expected_ports) THEN RAISE EXCEPTION 'runner expectation must be projected from the exact frozen handshake'; END IF;
  IF NOT EXISTS(SELECT 1 FROM project_assets a JOIN pci_topology_snapshots t ON t.project_id=a.project_id AND t.asset_id=a.id JOIN validation_wiring_batches b ON b.id=NEW.batch_id WHERE a.id=NEW.asset_id AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id AND a.version=(host->>'asset_version')::bigint AND t.id=NEW.topology_id AND t.scope_hash=host->>'topology_scope_hash' AND t.verdict='PASS' AND t.origin='real' AND t.valid_until>statement_timestamp() AND NEW.expectation->>'scope_hash'=b.facts_hash AND NEW.relations->>'scope_hash'=t.scope_hash AND NEW.relations->>'g1_id'=t.g1_id AND NEW.relations->>'step1_published'='true') THEN RAISE EXCEPTION 'Step 1 consumes current independent real topology and exact frozen relations'; END IF;
 ELSIF TG_TABLE_NAME='validation_wiring_step_reports' THEN
  IF NEW.version<>coalesce((SELECT max(version)+1 FROM validation_wiring_step_reports WHERE batch_id=NEW.batch_id AND asset_id=NEW.asset_id AND step=NEW.step),1) THEN RAISE EXCEPTION 'StepReport version must be monotonic'; END IF;
  IF NEW.step=1 THEN
   IF NEW.version<>1 OR NOT EXISTS(SELECT 1 FROM validation_wiring_batch_targets t WHERE t.batch_id=NEW.batch_id AND t.asset_id=NEW.asset_id AND NEW.detail->>'topology_id'=t.topology_id AND NEW.detail->'relations'=t.relations AND NEW.verdict=t.relations->>'effective_verdict') THEN RAISE EXCEPTION 'Step 1 must retain the exact topology relation projection'; END IF;
  ELSE
   IF NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports p WHERE p.id=NEW.parent_id AND p.batch_id=NEW.batch_id AND p.asset_id=NEW.asset_id AND p.step=NEW.step-1 AND p.verdict='PASS' AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=p.batch_id AND newer.asset_id=p.asset_id AND newer.step=p.step AND newer.version>p.version)) THEN RAISE EXCEPTION 'StepReport requires the current passing predecessor in the same scope'; END IF;
   -- The retained LLDP execution/receipt guard will be added with the adapter.
   -- Until then direct SQL cannot manufacture steps 2--4 either.
   RAISE EXCEPTION 'LLDP execution receipt binding is not yet available';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_batch_scope BEFORE INSERT ON validation_wiring_batches FOR EACH ROW EXECUTE FUNCTION keel_wiring_step_scope();
CREATE TRIGGER validation_wiring_batch_target_scope BEFORE INSERT ON validation_wiring_batch_targets FOR EACH ROW EXECUTE FUNCTION keel_wiring_step_scope();
CREATE TRIGGER validation_wiring_step_scope BEFORE INSERT ON validation_wiring_step_reports FOR EACH ROW EXECUTE FUNCTION keel_wiring_step_scope();
