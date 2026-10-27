-- A source snapshot keeps its original validity and immutable collection link.
ALTER TABLE monitor_metric_samples ADD COLUMN source_ref varchar(160) NOT NULL DEFAULT '';
ALTER TABLE monitor_metric_samples ADD COLUMN source_hash varchar(64) NOT NULL DEFAULT '';
ALTER TABLE monitor_metric_samples ADD COLUMN snapshot_only boolean NOT NULL DEFAULT false;
ALTER TABLE monitor_metric_samples ADD COLUMN source_valid_until timestamptz;
CREATE TABLE monitor_switch_source_receipts(
 frame_id varchar(64) PRIMARY KEY,
 run_id varchar(32) NOT NULL,
 switch_id varchar(32) NOT NULL,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 physical_port_id varchar(32) NOT NULL REFERENCES physical_ports(id),
 mapping_revision_id varchar(32) NOT NULL REFERENCES mapping_revisions(id),
 source_hash varchar(64) NOT NULL,
 sample_count integer NOT NULL CHECK(sample_count BETWEEN 1 AND 32),
 received_at timestamptz NOT NULL,
 FOREIGN KEY(run_id,switch_id) REFERENCES fabric_switch_samples(run_id,switch_id),
 UNIQUE(run_id,switch_id,physical_port_id)
);
CREATE TRIGGER monitor_switch_source_receipt_immutable BEFORE UPDATE OR DELETE ON monitor_switch_source_receipts FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE FUNCTION keel_monitor_switch_source_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM fabric_collection_runs run JOIN fabric_switch_samples sample ON sample.run_id=run.id JOIN mapping_revisions revision ON revision.id=NEW.mapping_revision_id JOIN mapping_assessments assessment ON assessment.id=revision.assessment_id CROSS JOIN LATERAL jsonb_array_elements(assessment.candidate->'bindings') binding WHERE run.id=NEW.run_id AND sample.switch_id=NEW.switch_id AND run.project_id=NEW.project_id AND sample.content_hash=NEW.source_hash AND revision.project_id=NEW.project_id AND revision.machine_id=NEW.machine_id AND binding#>>'{plan,physical_port_id}'=NEW.physical_port_id AND binding#>>'{observed,switch_id}'=NEW.switch_id AND EXISTS(SELECT 1 FROM jsonb_array_elements(sample.sample->'ports') port WHERE port->>'name'=binding#>>'{observed,switch_port}')) THEN RAISE EXCEPTION 'switch monitoring must retain the original collection and exact G3 physical peer'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER monitor_switch_source_scope BEFORE INSERT ON monitor_switch_source_receipts FOR EACH ROW EXECUTE FUNCTION keel_monitor_switch_source_scope();
-- Exact projection whitelist: a receipt cannot authorize invented metric values.
CREATE FUNCTION keel_monitor_switch_projection(port jsonb, metric text, entity text) RETURNS jsonb LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE value jsonb := 'null'::jsonb; state text := ''; quality text := 'unknown';
BEGIN
 CASE metric
 WHEN 'link.state' THEN
  IF jsonb_typeof(port->'link_up')='boolean' THEN
   state := CASE WHEN (port->>'link_up')::boolean THEN 'up' ELSE 'down' END;
  END IF;
 WHEN 'link.speed' THEN value := coalesce(port->'speed_mbps','null'::jsonb);
 WHEN 'dom.temperature' THEN value := coalesce(port#>'{optics,temperature_c}','null'::jsonb);
 WHEN 'dom.rx_power' THEN value := coalesce(port#>'{optics,rx_dbm}','null'::jsonb);
 WHEN 'dom.tx_power' THEN value := coalesce(port#>'{optics,tx_dbm}','null'::jsonb);
 ELSE RETURN NULL;
 END CASE;
 IF value<>'null'::jsonb OR state<>'' THEN quality := 'good'; END IF;
 RETURN jsonb_build_object('key',metric,'entity',entity,'value',value,'state',state,'quality',quality);
END $$;
CREATE FUNCTION keel_monitor_sample_source_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.source='switch' THEN
  IF NOT EXISTS(SELECT 1 FROM monitor_switch_source_receipts receipt JOIN fabric_collection_runs run ON run.id=receipt.run_id JOIN fabric_switch_samples sample ON sample.run_id=receipt.run_id AND sample.switch_id=receipt.switch_id WHERE receipt.frame_id=NEW.frame_id AND receipt.project_id=NEW.project_id AND receipt.machine_id=NEW.machine_id AND receipt.physical_port_id=NEW.physical_port_id AND receipt.mapping_revision_id=NEW.mapping_revision_id AND receipt.source_hash=NEW.source_hash AND NEW.source_ref='fabric:'||receipt.run_id||':'||receipt.switch_id AND NEW.method='collector' AND NEW.credential_id IS NULL AND NEW.snapshot_only AND NEW.source_valid_until=LEAST(run.valid_until,keel_wiring_original_timestamp(sample.sample->>'collected_at')+interval '5 minutes') AND NEW.observed_at=keel_wiring_original_timestamp(sample.sample->>'collected_at') AND NEW.origin=CASE WHEN run.report->>'evidence_source'='device' THEN 'real' ELSE 'sim' END AND NEW.entity_key=receipt.physical_port_id AND NEW.sample->>'key'=NEW.metric_key AND keel_wiring_original_timestamp(NEW.sample->>'observed_at')=NEW.observed_at AND EXISTS(SELECT 1 FROM mapping_revisions revision JOIN mapping_assessments assessment ON assessment.id=revision.assessment_id CROSS JOIN LATERAL jsonb_array_elements(assessment.candidate->'bindings') binding CROSS JOIN LATERAL jsonb_array_elements(sample.sample->'ports') port WHERE revision.id=receipt.mapping_revision_id AND binding#>>'{plan,physical_port_id}'=receipt.physical_port_id AND binding#>>'{observed,switch_id}'=receipt.switch_id AND port->>'name'=binding#>>'{observed,switch_port}' AND NEW.sample-'observed_at'=keel_monitor_switch_projection(port,NEW.metric_key,receipt.physical_port_id))) THEN RAISE EXCEPTION 'switch sample must retain original time, origin, validity and collection receipt'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER monitor_sample_source_scope BEFORE INSERT ON monitor_metric_samples FOR EACH ROW EXECUTE FUNCTION keel_monitor_sample_source_scope();
