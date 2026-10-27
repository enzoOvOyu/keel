-- WP-2.4 target-scoped readiness only. This table grants no execution,
-- publishes no StepReport/G2/NP-3, and cannot accept a client online verdict.
CREATE TABLE validation_wiring_handshakes(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 facts_hash varchar(64) NOT NULL CHECK(facts_hash ~ '^[0-9a-f]{64}$'),
 facts jsonb NOT NULL,
 collector jsonb NOT NULL,
 readiness_at timestamptz NOT NULL,
 valid_until timestamptz NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL,
 CHECK((facts->>'version'='keel.wiring-scope/v1' AND facts->>'project_id'=project_id AND facts->>'facts_hash'=facts_hash AND facts->>'ready'='true' AND jsonb_typeof(facts->'targets')='array' AND jsonb_array_length(facts->'targets') BETWEEN 1 AND 200 AND jsonb_typeof(facts->'switches')='array' AND jsonb_array_length(facts->'switches') BETWEEN 1 AND 4096 AND facts->'blockers'='[]'::jsonb AND (facts->>'ttl_seconds')::int BETWEEN 1 AND 30 AND facts->>'collector_hash' ~ '^[0-9a-f]{64}$') IS TRUE),
 CHECK((collector->>'service'='collector' AND collector->>'state'='ready' AND collector->>'code'='protocol_verified') IS TRUE),
 CHECK(readiness_at<=created_at AND valid_until>created_at AND valid_until<=readiness_at+interval '30 seconds'),
 CHECK((valid_until<=readiness_at+make_interval(secs=>(facts->>'ttl_seconds')::int) AND valid_until<=(facts->>'evidence_valid_until')::timestamptz) IS TRUE)
);
CREATE INDEX validation_wiring_handshake_history ON validation_wiring_handshakes(project_id,created_at DESC,id DESC);
CREATE TRIGGER validation_wiring_handshake_immutable BEFORE UPDATE OR DELETE ON validation_wiring_handshakes FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_wiring_handshake_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target jsonb; port jsonb; device jsonb; total_ports integer:=0;
BEGIN
 IF NEW.readiness_at>statement_timestamp() OR NEW.valid_until<=statement_timestamp() THEN RAISE EXCEPTION 'handshake must have an unexpired server readiness lease'; END IF;
 IF NOT EXISTS(SELECT 1 FROM fabric_collection_runs r WHERE r.id=NEW.facts->>'collection_id' AND r.project_id=NEW.project_id AND r.package_id=NEW.facts->>'network_package_id' AND r.report->>'evidence_source'='device' AND r.valid_until>=NEW.valid_until) THEN RAISE EXCEPTION 'handshake requires current real switch collection'; END IF;
 IF (SELECT count(DISTINCT t->>'asset_id') FROM jsonb_array_elements(NEW.facts->'targets') t)<>jsonb_array_length(NEW.facts->'targets') THEN RAISE EXCEPTION 'handshake targets must be unique'; END IF;
 FOR target IN SELECT value FROM jsonb_array_elements(NEW.facts->'targets') LOOP
  IF NOT EXISTS(SELECT 1 FROM project_assets a JOIN machines m ON m.id=a.machine_id JOIN pci_topology_snapshots t ON t.project_id=a.project_id AND t.asset_id=a.id JOIN validation_gate_evidence g ON g.id=t.g1_id JOIN validation_precollect_requests q ON q.id=t.precollect_id WHERE a.id=target->>'asset_id' AND a.project_id=NEW.project_id AND a.machine_id=target->>'machine_id' AND a.version=(target->>'asset_version')::bigint AND m.serial_number=target->>'serial' AND a.hostname=target->>'hostname' AND t.id=target->>'topology_id' AND t.scope_hash=target->>'topology_scope_hash' AND t.precollect_id=target->>'precollect_id' AND t.g1_id=target->>'g1_id' AND t.origin='real' AND t.verdict='PASS' AND t.valid_until>=NEW.valid_until AND g.verdict='PASS' AND g.valid_until>=NEW.valid_until AND q.scope->>'bom_package_id'=target->>'bom_package_id' AND NOT EXISTS(SELECT 1 FROM pci_topology_snapshots newer WHERE newer.project_id=a.project_id AND newer.asset_id=a.id AND newer.version>t.version) AND NOT EXISTS(SELECT 1 FROM validation_gate_evidence newer WHERE newer.project_id=a.project_id AND newer.asset_id=a.id AND (newer.created_at,newer.id)>(g.created_at,g.id))) THEN RAISE EXCEPTION 'handshake requires current real G1 and topology for every selected host'; END IF;
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
CREATE TRIGGER validation_wiring_handshake_scope BEFORE INSERT ON validation_wiring_handshakes FOR EACH ROW EXECUTE FUNCTION keel_wiring_handshake_scope();
