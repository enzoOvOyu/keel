-- Scoped G1 waivers never overwrite original evidence or authorize missing
-- physical identity, simulated data, stale captures, trust or resource bypass.
CREATE TABLE gate_waivers(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 gate varchar(2) NOT NULL CHECK(gate='G1'),
 evidence_id varchar(32) NOT NULL REFERENCES validation_gate_evidence(id),
 source_hash char(64) NOT NULL,
 scope_hash char(64) NOT NULL,
 revision_fingerprint char(64) NOT NULL,
 policy_id varchar(32) NOT NULL REFERENCES param_keys(id),
 policy_hash char(64) NOT NULL,
 findings jsonb NOT NULL CHECK(jsonb_typeof(findings)='array' AND jsonb_array_length(findings)>0),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 remediation_plan text NOT NULL CHECK(length(btrim(remediation_plan)) BETWEEN 3 AND 4000),
 approved_by varchar(32) NOT NULL REFERENCES users(id),
 approved_at timestamptz NOT NULL,
 valid_until timestamptz NOT NULL CHECK(valid_until>approved_at),
 UNIQUE(project_id,asset_id,source_hash,approved_by,approved_at)
);
CREATE TRIGGER gate_waiver_immutable BEFORE UPDATE OR DELETE ON gate_waivers FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TABLE gate_waiver_revocations(
 id varchar(32) PRIMARY KEY,
 waiver_id varchar(32) NOT NULL UNIQUE REFERENCES gate_waivers(id),
 revoked_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL
);
CREATE TRIGGER gate_waiver_revocation_immutable BEFORE UPDATE OR DELETE ON gate_waiver_revocations FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE INDEX gate_waiver_scope ON gate_waivers(project_id,asset_id,evidence_id,approved_at DESC,id DESC);

-- A missing/retired policy admits no waiver. Exact fields must be explicitly
-- enabled in the existing versioned parameter catalog; no wildcard matching.
CREATE FUNCTION keel_g1_waiver_policy(project varchar) RETURNS TABLE(policy_id varchar,policy_hash text,fields jsonb) LANGUAGE sql STABLE AS $$
 SELECT p.id,encode(sha256(convert_to(p.id||':'||extract(epoch FROM p.updated_at)::text||':'||p.state||':'||p.default_value,'UTF8')),'hex'),p.default_value::jsonb
 FROM param_keys p WHERE p.key='gate.g1.waiver_fields' AND p.scope_key IN('global','project:'||project) AND p.state='active'
 AND NOT EXISTS(SELECT 1 FROM param_keys override WHERE override.key=p.key AND override.scope_key='project:'||project AND p.scope_key='global')
$$;
CREATE FUNCTION keel_current_g1_waiver(evidence varchar) RETURNS varchar LANGUAGE sql VOLATILE AS $$
 SELECT w.id FROM gate_waivers w JOIN validation_gate_evidence g ON g.id=w.evidence_id
 JOIN validation_precollect_records r ON r.id=g.precollect_id
 JOIN validation_precollect_requests q ON q.id=r.id
 JOIN project_assets a ON a.id=g.asset_id AND a.project_id=g.project_id
 JOIN projects project ON project.id=g.project_id
 JOIN users approver ON approver.id=w.approved_by AND approver.enabled
 JOIN LATERAL keel_g1_waiver_policy(g.project_id) policy ON policy.policy_id=w.policy_id AND policy.policy_hash=w.policy_hash
 WHERE g.id=evidence AND w.id=(SELECT latest.id FROM gate_waivers latest WHERE latest.evidence_id=g.id ORDER BY latest.approved_at DESC,latest.id DESC LIMIT 1) AND g.gate='G1' AND g.verdict='BLOCKED' AND g.valid_until>statement_timestamp() AND r.origin='real'
 AND w.project_id=g.project_id AND w.asset_id=g.asset_id AND w.scope_hash=g.scope_hash AND w.revision_fingerprint=g.revision_fingerprint
 AND w.findings=g.evaluation->'findings' AND w.valid_until>statement_timestamp() AND w.valid_until<=g.valid_until
 AND a.version=(q.scope->>'asset_version')::bigint AND a.model_version_id=q.scope->>'model_version_id'
 AND a.state NOT IN('planned','provisioning','failed','maintained') AND project.state NOT IN('archived','delivered')
 AND (approver.is_admin OR EXISTS(SELECT 1 FROM memberships member WHERE member.project_id=g.project_id AND member.user_id=approver.id AND member.role IN('Admin','DL')))
 AND NOT EXISTS(SELECT 1 FROM gate_waiver_revocations revoked WHERE revoked.waiver_id=w.id)
 AND NOT EXISTS(SELECT 1 FROM validation_gate_evidence newer WHERE newer.project_id=g.project_id AND newer.asset_id=g.asset_id AND (newer.created_at,newer.id)>(g.created_at,g.id))
 AND jsonb_typeof(policy.fields)='array' AND jsonb_array_length(w.findings)>0
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(policy.fields) field WHERE (jsonb_typeof(field)='string' AND field#>>'{}' IN('nic.dom','nic.driver_version','gpu.hbm_ecc_observation','system.os_release')) IS NOT TRUE)
 AND (SELECT count(DISTINCT field) FROM jsonb_array_elements(policy.fields) field)=jsonb_array_length(policy.fields)
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(w.findings) finding WHERE (finding->>'code'='PRECOLLECT_FIELD_MISSING' AND finding->>'verdict'='BLOCKED' AND finding->>'field' IN('nic.dom','nic.driver_version','gpu.hbm_ecc_observation','system.os_release') AND policy.fields ? (finding->>'field')) IS NOT TRUE)
 ORDER BY w.approved_at DESC,w.id DESC LIMIT 1
$$;
CREATE FUNCTION keel_g1_evidence_accepted(evidence varchar,waiver varchar,until_at timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM validation_gate_evidence g WHERE g.id=evidence AND g.valid_until>=until_at AND g.valid_until>statement_timestamp()
 AND ((g.verdict='PASS' AND coalesce(waiver,'')='') OR (g.verdict='BLOCKED' AND waiver=keel_current_g1_waiver(evidence) AND EXISTS(SELECT 1 FROM gate_waivers w WHERE w.id=waiver AND w.valid_until>=until_at))))
$$;

CREATE OR REPLACE FUNCTION keel_pci_topology_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='pci_topology_snapshots' THEN
  IF NOT EXISTS(SELECT 1 FROM validation_gate_evidence g JOIN validation_precollect_requests q ON q.id=g.precollect_id JOIN validation_precollect_records r ON r.id=q.id JOIN project_assets a ON a.id=q.asset_id JOIN device_model_versions m ON m.id=NEW.model_version_id WHERE g.id=NEW.g1_id AND g.project_id=NEW.project_id AND g.asset_id=NEW.asset_id AND g.machine_id=NEW.machine_id AND g.precollect_id=NEW.precollect_id AND keel_g1_evidence_accepted(g.id,NEW.scope->>'g1_waiver_id',NEW.valid_until) AND g.valid_until>statement_timestamp() AND NEW.valid_until<=g.valid_until AND NEW.valid_until>statement_timestamp() AND q.scope_hash=NEW.scope->>'precollect_scope_hash' AND r.raw_sha256=NEW.raw_sha256 AND r.origin=NEW.origin AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id AND a.model_version_id=m.id AND a.version=(q.scope->>'asset_version')::bigint AND m.content_hash=NEW.scope->>'model_content_hash' AND m.expected_topology=NEW.scope->'expectation' AND r.report->'topology'=NEW.snapshot AND (NEW.verdict<>'PASS' OR NEW.origin='real') AND NOT EXISTS(SELECT 1 FROM validation_gate_evidence newer WHERE newer.project_id=g.project_id AND newer.asset_id=g.asset_id AND (newer.created_at,newer.id)>(g.created_at,g.id))) THEN RAISE EXCEPTION 'PCIe topology requires current G1, independent model and original retained capture'; END IF;
 ELSIF TG_TABLE_NAME='topology_issue_bindings' THEN
  IF NOT EXISTS(SELECT 1 FROM issues i JOIN project_assets a ON a.id=NEW.asset_id WHERE i.id=NEW.issue_id AND i.project_id=NEW.project_id AND i.source='topology' AND i.category='T7' AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id) THEN RAISE EXCEPTION 'T7 binding must remain in the original asset scope'; END IF;
 ELSIF TG_TABLE_NAME='topology_diffs' THEN
  IF NEW.issue_id IS NOT NULL AND (NEW.detail->>'verdict'<>'FAIL' OR NOT EXISTS(SELECT 1 FROM pci_topology_snapshots s JOIN topology_issue_bindings b ON b.issue_id=NEW.issue_id WHERE s.id=NEW.snapshot_id AND b.project_id=s.project_id AND b.asset_id=s.asset_id AND b.machine_id=s.machine_id AND b.model_version_id=s.model_version_id AND b.origin=s.origin)) THEN RAISE EXCEPTION 'T7 observation must match the snapshot scope'; END IF;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION keel_wiring_handshake_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target jsonb; port jsonb; device jsonb; total_ports integer:=0;
BEGIN
 IF NEW.readiness_at>statement_timestamp() OR NEW.valid_until<=statement_timestamp() THEN RAISE EXCEPTION 'handshake must have an unexpired server readiness lease'; END IF;
 IF NOT EXISTS(SELECT 1 FROM fabric_collection_runs r WHERE r.id=NEW.facts->>'collection_id' AND r.project_id=NEW.project_id AND r.package_id=NEW.facts->>'network_package_id' AND r.report->>'evidence_source'='device' AND r.valid_until>=NEW.valid_until) THEN RAISE EXCEPTION 'handshake requires current real switch collection'; END IF;
 IF (SELECT count(DISTINCT t->>'asset_id') FROM jsonb_array_elements(NEW.facts->'targets') t)<>jsonb_array_length(NEW.facts->'targets') THEN RAISE EXCEPTION 'handshake targets must be unique'; END IF;
 FOR target IN SELECT value FROM jsonb_array_elements(NEW.facts->'targets') LOOP
  IF NOT EXISTS(SELECT 1 FROM project_assets a JOIN machines m ON m.id=a.machine_id JOIN pci_topology_snapshots t ON t.project_id=a.project_id AND t.asset_id=a.id JOIN validation_gate_evidence g ON g.id=t.g1_id JOIN validation_precollect_requests q ON q.id=t.precollect_id WHERE a.id=target->>'asset_id' AND a.project_id=NEW.project_id AND a.machine_id=target->>'machine_id' AND a.version=(target->>'asset_version')::bigint AND m.serial_number=target->>'serial' AND a.hostname=target->>'hostname' AND t.id=target->>'topology_id' AND t.scope_hash=target->>'topology_scope_hash' AND t.precollect_id=target->>'precollect_id' AND t.g1_id=target->>'g1_id' AND t.origin='real' AND t.verdict='PASS' AND t.valid_until>=NEW.valid_until AND keel_g1_evidence_accepted(g.id,target->>'g1_waiver_id',NEW.valid_until) AND g.valid_until>=NEW.valid_until AND q.scope->>'bom_package_id'=target->>'bom_package_id' AND NOT EXISTS(SELECT 1 FROM pci_topology_snapshots newer WHERE newer.project_id=a.project_id AND newer.asset_id=a.id AND newer.version>t.version) AND NOT EXISTS(SELECT 1 FROM validation_gate_evidence newer WHERE newer.project_id=a.project_id AND newer.asset_id=a.id AND (newer.created_at,newer.id)>(g.created_at,g.id))) THEN RAISE EXCEPTION 'handshake requires current real G1 and topology for every selected host'; END IF;
  IF NOT (jsonb_typeof(target->'ports')='array' AND jsonb_array_length(target->'ports') BETWEEN 1 AND 128) IS TRUE THEN RAISE EXCEPTION 'explicit bounded target ports required'; END IF;
  IF (SELECT count(DISTINCT p->>'physical_port_id') FROM jsonb_array_elements(target->'ports') p)<>jsonb_array_length(target->'ports') THEN RAISE EXCEPTION 'duplicate host ports'; END IF;
  total_ports:=total_ports+jsonb_array_length(target->'ports');
  FOR port IN SELECT value FROM jsonb_array_elements(target->'ports') LOOP
   IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(NEW.facts->'switches') d WHERE d->>'id'=port->>'switch_id' AND d->>'serial'=port->>'switch_serial' AND (d->'ports') ? (port->>'switch_port')) THEN RAISE EXCEPTION 'every host port requires a selected switch readiness proof'; END IF;
   IF NOT EXISTS(SELECT 1 FROM validation_precollect_ports observed JOIN physical_ports p ON p.id=observed.physical_port_id JOIN bom_items cable ON cable.item_uid=port->>'cable_id' AND cable.category='cable' AND cable.status<>'retired' JOIN bom_item_revisions revision ON revision.item_uid=cable.item_uid AND revision.project_id=NEW.project_id AND revision.package_id=NEW.facts->>'network_package_id' JOIN physical_ports peer ON peer.id=port->>'switch_port_id' JOIN switches sw ON sw.id=peer.switch_id WHERE observed.record_id=target->>'precollect_id' AND observed.physical_port_id=port->>'physical_port_id' AND observed.component_id=port->>'component_id' AND p.machine_id=target->>'machine_id' AND observed.observation->>'interface'=port->>'interface' AND length(port->>'interface')>0 AND observed.observation->>'bdf'=port->>'bdf' AND observed.observation->>'permanent_mac'=port->>'permanent_mac' AND sw.id=port->>'switch_id' AND sw.serial_number=port->>'switch_serial' AND ((cable.endpoint_a_uid=p.id AND cable.endpoint_b_uid=peer.id) OR (cable.endpoint_b_uid=p.id AND cable.endpoint_a_uid=peer.id))) THEN RAISE EXCEPTION 'handshake port must retain frozen observation and BOM cable endpoint identities'; END IF;
  END LOOP;
 END LOOP;
 IF (SELECT count(DISTINCT p->>'physical_port_id') FROM jsonb_array_elements(NEW.facts->'targets') t CROSS JOIN LATERAL jsonb_array_elements(t->'ports') p)<>total_ports OR (SELECT count(DISTINCT p->>'switch_port_id') FROM jsonb_array_elements(NEW.facts->'targets') t CROSS JOIN LATERAL jsonb_array_elements(t->'ports') p)<>total_ports THEN RAISE EXCEPTION 'selected endpoints must be unambiguous'; END IF;
 IF total_ports>4096 THEN RAISE EXCEPTION 'handshake port bound exceeded'; END IF;
 FOR device IN SELECT value FROM jsonb_array_elements(NEW.facts->'switches') LOOP
  IF NOT (device#>>'{np1,state}'='PASS' AND device#>>'{np2,state}'='PASS' AND device#>>'{np4,state}'='PASS' AND device->>'management_hash' ~ '^[0-9a-f]{64}$' AND device->>'golden_hash' ~ '^[0-9a-f]{64}$') IS TRUE THEN RAISE EXCEPTION 'target switch requires NP-1/NP-2/NP-4'; END IF;
  IF NOT EXISTS(SELECT 1 FROM fabric_switch_samples s JOIN switches sw ON sw.id=s.switch_id WHERE s.run_id=NEW.facts->>'collection_id' AND s.switch_id=device->>'id' AND s.content_hash=device->>'sample_hash' AND sw.serial_number=device->>'serial') THEN RAISE EXCEPTION 'target switch evidence changed'; END IF;
 END LOOP;
 RETURN NEW;
END $$;

CREATE FUNCTION keel_g1_waiver_write_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='gate_waivers' THEN
  IF NEW.approved_at<statement_timestamp()-interval '5 seconds' OR NEW.approved_at>statement_timestamp()+interval '1 second' OR keel_current_g1_waiver(NEW.evidence_id) IS DISTINCT FROM NEW.id THEN
   RAISE EXCEPTION 'G1 waiver requires current real evidence, allowed exact missing fields, DL approval and original scope/TTL';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM gate_waivers w JOIN users u ON u.id=NEW.revoked_by AND u.enabled WHERE w.id=NEW.waiver_id AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships member WHERE member.project_id=w.project_id AND member.user_id=u.id AND member.role IN('Admin','DL')))) THEN RAISE EXCEPTION 'waiver revocation requires current project DL'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER gate_waiver_write_scope AFTER INSERT ON gate_waivers FOR EACH ROW EXECUTE FUNCTION keel_g1_waiver_write_scope();
CREATE TRIGGER gate_waiver_revocation_scope BEFORE INSERT ON gate_waiver_revocations FOR EACH ROW EXECUTE FUNCTION keel_g1_waiver_write_scope();
