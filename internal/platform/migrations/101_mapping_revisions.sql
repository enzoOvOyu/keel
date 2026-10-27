-- G3 is a publication of verified stable identities, never another capture.
CREATE TABLE mapping_assessments(
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 scope_hash char(64) NOT NULL CHECK(scope_hash ~ '^[0-9a-f]{64}$'),
 candidate jsonb NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 3 AND 1000),
 created_at timestamptz NOT NULL,
 CHECK((candidate->>'rule_version'='keel.mapping/v1' AND candidate->>'project_id'=project_id AND candidate->>'asset_id'=asset_id AND candidate->>'machine_id'=machine_id AND candidate->>'scope_hash'=scope_hash AND candidate->>'verdict' IN('PASS','BLOCKED') AND jsonb_typeof(candidate->'bindings')='array' AND jsonb_typeof(candidate->'conflicts')='array') IS TRUE)
);
CREATE TABLE mapping_revisions(
 id varchar(32) PRIMARY KEY,
 assessment_id varchar(32) NOT NULL UNIQUE REFERENCES mapping_assessments(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 asset_id varchar(32) NOT NULL REFERENCES project_assets(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 version bigint NOT NULL CHECK(version>0),
 previous_id varchar(32) REFERENCES mapping_revisions(id),
 confirmed_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id,asset_id,version)
);
CREATE INDEX mapping_assessment_history ON mapping_assessments(project_id,asset_id,created_at DESC,id DESC);
CREATE TRIGGER mapping_assessment_immutable BEFORE UPDATE OR DELETE ON mapping_assessments FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TRIGGER mapping_revision_immutable BEFORE UPDATE OR DELETE ON mapping_revisions FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();

CREATE FUNCTION keel_mapping_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source mapping_assessments; frozen jsonb; binding jsonb; gpu jsonb; original jsonb; port_count integer; gpu_count integer; nic_count integer; prior varchar;
BEGIN
 IF TG_TABLE_NAME='mapping_assessments' THEN
  IF NOT EXISTS(SELECT 1 FROM project_assets a JOIN projects p ON p.id=a.project_id JOIN users u ON u.id=NEW.created_by WHERE a.id=NEW.asset_id AND a.project_id=NEW.project_id AND a.machine_id=NEW.machine_id AND p.state NOT IN('archived','delivered') AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=p.id AND m.role IN('Admin','DL')))) THEN RAISE EXCEPTION 'mapping assessment requires current project scope and DL authority'; END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO source FROM mapping_assessments WHERE id=NEW.assessment_id;
 frozen:=source.candidate;
 IF source.id IS NULL OR NEW.project_id IS DISTINCT FROM source.project_id OR NEW.asset_id IS DISTINCT FROM source.asset_id OR NEW.machine_id IS DISTINCT FROM source.machine_id OR NEW.confirmed_by IS DISTINCT FROM source.created_by OR NOT (frozen->>'ready'='true' AND frozen->>'verdict'='PASS' AND jsonb_array_length(frozen->'conflicts')=0 AND keel_wiring_original_timestamp(frozen->>'valid_until')>clock_timestamp()) IS TRUE THEN RAISE EXCEPTION 'mapping revision requires a fresh complete passing assessment'; END IF;
 SELECT id INTO prior FROM mapping_revisions WHERE project_id=NEW.project_id AND asset_id=NEW.asset_id ORDER BY version DESC LIMIT 1;
 IF NEW.previous_id IS DISTINCT FROM prior OR NEW.version<>coalesce((SELECT max(version)+1 FROM mapping_revisions WHERE project_id=NEW.project_id AND asset_id=NEW.asset_id),1) THEN RAISE EXCEPTION 'mapping versions must retain the original immutable history'; END IF;
 IF NOT EXISTS(SELECT 1 FROM project_assets asset JOIN pci_topology_snapshots topology ON topology.asset_id=asset.id AND topology.project_id=asset.project_id JOIN validation_precollect_requests request ON request.id=topology.precollect_id JOIN device_model_versions model ON model.id=topology.model_version_id JOIN users u ON u.id=NEW.confirmed_by WHERE asset.id=NEW.asset_id AND asset.project_id=NEW.project_id AND asset.machine_id=NEW.machine_id AND asset.state IN('lldp','mapped','configured','netverified','tested') AND asset.model_version_id=frozen->>'model_version_id' AND asset.version=(frozen->>'asset_version')::bigint AND topology.id=frozen->>'topology_id' AND topology.g1_id=frozen->>'g1_id' AND topology.model_version_id=asset.model_version_id AND model.content_hash=frozen->>'model_content_hash' AND topology.verdict='PASS' AND topology.origin='real' AND topology.valid_until>=keel_wiring_original_timestamp(frozen->>'valid_until') AND request.scope->>'bom_package_id'=frozen->>'bom_package_id' AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=asset.project_id AND m.role IN('Admin','DL')))) THEN RAISE EXCEPTION 'mapping must consume the current independent real model and topology'; END IF;
 IF EXISTS(SELECT 1 FROM topology_issue_bindings b JOIN issues i ON i.id=b.issue_id WHERE b.project_id=NEW.project_id AND b.asset_id=NEW.asset_id AND b.origin='real' AND i.state<>'closed') OR EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN issues i ON i.id=b.issue_id WHERE b.project_id=NEW.project_id AND b.asset_id=NEW.asset_id AND b.host_origin='real' AND b.switch_origin='device' AND i.state<>'closed' AND EXISTS(SELECT 1 FROM jsonb_array_elements(frozen->'bindings') p WHERE p#>>'{plan,physical_port_id}'=b.physical_port_id)) THEN RAISE EXCEPTION 'mapping cannot bypass unresolved real topology or wiring issues'; END IF;
 SELECT count(*),count(DISTINCT c.id) INTO port_count,nic_count FROM physical_ports p JOIN components c ON c.id=p.parent_uid AND c.category='nic' JOIN bom_items i ON i.item_uid=p.id AND i.status<>'retired' JOIN bom_items parent ON parent.item_uid=c.id AND parent.status<>'retired' JOIN bom_item_revisions r ON r.item_uid=p.id AND r.project_id=NEW.project_id AND r.package_id=frozen->>'bom_package_id' WHERE p.machine_id=NEW.machine_id AND coalesce(r.row_snapshot#>>'{values,port_role}','')<>'unused';
 SELECT count(*) INTO gpu_count FROM validation_precollect_requests q JOIN pci_topology_snapshots t ON t.precollect_id=q.id CROSS JOIN LATERAL jsonb_array_elements(q.scope->'components') c WHERE t.id=frozen->>'topology_id' AND c->>'category'='gpu';
 IF NOT (port_count>0 AND jsonb_array_length(frozen->'bindings')=port_count AND (frozen#>>'{coverage,required_ports}')::int=port_count AND (frozen#>>'{coverage,mapped_ports}')::int=port_count AND (frozen#>>'{coverage,required_nics}')::int=nic_count AND (frozen#>>'{coverage,mapped_nics}')::int=nic_count AND (frozen#>>'{coverage,required_gpus}')::int=gpu_count AND (frozen#>>'{coverage,mapped_gpus}')::int=gpu_count) IS TRUE THEN RAISE EXCEPTION 'mapping coverage cannot shrink the enabled GPU/NIC/port denominator'; END IF;
 IF (SELECT count(DISTINCT b#>>'{plan,physical_port_id}') FROM jsonb_array_elements(frozen->'bindings') b)<>port_count OR (SELECT count(DISTINCT b->>'logical_name') FROM jsonb_array_elements(frozen->'bindings') b)<>port_count OR (SELECT count(DISTINCT b#>>'{observed,interface}') FROM jsonb_array_elements(frozen->'bindings') b)<>port_count OR (SELECT count(DISTINCT b#>>'{observed,permanent_mac}') FROM jsonb_array_elements(frozen->'bindings') b)<>port_count OR (SELECT count(DISTINCT b->>'switch_port_id') FROM jsonb_array_elements(frozen->'bindings') b)<>port_count THEN RAISE EXCEPTION 'mapping contains conflicting stable identities or logical names'; END IF;
 FOR binding IN SELECT value FROM jsonb_array_elements(frozen->'bindings') LOOP
  IF NOT EXISTS(SELECT 1 FROM physical_ports p JOIN components c ON c.id=p.parent_uid AND c.category='nic' JOIN bom_item_revisions r ON r.item_uid=p.id AND r.project_id=NEW.project_id AND r.package_id=frozen->>'bom_package_id' JOIN validation_wiring_confirmed_ports confirmed ON confirmed.physical_port_id=p.id JOIN validation_wiring_confirmations g ON g.id=confirmed.confirmation_id JOIN validation_wiring_step_reports three ON three.id=g.step3_id JOIN validation_wiring_step_reports two ON two.id=three.parent_id JOIN validation_wiring_batch_targets target ON target.batch_id=g.batch_id AND target.asset_id=g.asset_id WHERE p.id=binding#>>'{plan,physical_port_id}' AND p.machine_id=NEW.machine_id AND c.id=binding#>>'{plan,component_id}' AND coalesce(c.serial_number,'')=binding#>>'{plan,component_serial}' AND r.row_snapshot#>>'{values,port_index}'=binding#>>'{plan,port_index}' AND r.row_snapshot#>>'{values,port_role}'=binding#>>'{plan,role}' AND binding#>>'{plan,role}' IN('management','storage','compute') AND coalesce(r.row_snapshot#>>'{values,rail}','')=binding#>>'{plan,rail}' AND g.id=binding->>'confirmation_id' AND g.project_id=NEW.project_id AND g.asset_id=NEW.asset_id AND g.valid_until>=keel_wiring_original_timestamp(frozen->>'valid_until') AND confirmed.expected=binding->'observed' AND confirmed.switch_port_id=binding->>'switch_port_id' AND confirmed.expected->>'physical_port_id'=p.id AND confirmed.expected->>'component_id'=c.id AND confirmed.expected->>'rail'=binding#>>'{plan,rail}' AND confirmed.expected->>'port_index'=binding#>>'{plan,port_index}' AND coalesce(r.row_snapshot#>>'{values,port_name}','')=binding#>>'{plan,name}' AND binding->>'logical_name'=CASE WHEN coalesce(r.row_snapshot#>>'{values,port_name}','')<>'' THEN r.row_snapshot#>>'{values,port_name}' ELSE 'nic-'||coalesce(nullif(r.row_snapshot#>>'{values,rail}',''),r.row_snapshot#>>'{values,port_role}')||'-p'||(r.row_snapshot#>>'{values,port_index}') END AND binding->>'logical_name' ~ '^[A-Za-z0-9][A-Za-z0-9_.:-]{0,62}$' AND three.verdict='PASS' AND two.verdict='PASS' AND target.topology_id=frozen->>'topology_id' AND NOT EXISTS(SELECT 1 FROM validation_wiring_step_reports newer WHERE newer.batch_id=three.batch_id AND newer.asset_id=three.asset_id AND ((newer.step=3 AND newer.version>three.version) OR (newer.step=2 AND newer.version>two.version))) AND NOT EXISTS(SELECT 1 FROM validation_wiring_confirmed_ports next_port JOIN validation_wiring_confirmations next_g ON next_g.id=next_port.confirmation_id WHERE next_g.project_id=g.project_id AND next_g.asset_id=g.asset_id AND next_port.physical_port_id=p.id AND (next_g.created_at,next_g.id)>(g.created_at,g.id))) THEN RAISE EXCEPTION 'mapping binding must retain exact BOM identities and current G2 physical endpoints'; END IF;
  IF binding#>>'{plan,role}'='compute' AND jsonb_array_length(binding->'gpus')=0 THEN RAISE EXCEPTION 'enabled compute ports require independent GPU relations'; END IF;
  FOR gpu IN SELECT value FROM jsonb_array_elements(binding->'gpus') LOOP
   IF NOT EXISTS(SELECT 1 FROM validation_wiring_confirmations g JOIN validation_wiring_batch_targets target ON target.batch_id=g.batch_id AND target.asset_id=g.asset_id CROSS JOIN LATERAL jsonb_array_elements(target.relations->'rows') r WHERE g.id=binding->>'confirmation_id' AND target.topology_id=frozen->>'topology_id' AND r->>'verdict'='PASS' AND r->>'physical_port_id'=binding#>>'{plan,physical_port_id}' AND r->>'nic_component_id'=binding#>>'{plan,component_id}' AND r->>'gpu_component_id'=gpu->>'component_id' AND r->>'gpu_key'=gpu->>'model_key' AND r->>'gpu_uuid'=gpu->>'uuid' AND r#>>'{gpu_node,bdf}'=gpu->>'bdf' AND r#>>'{nic_node,bdf}'=binding#>>'{observed,bdf}' AND r->>'expected_affinity'=gpu->>'affinity' AND r->>'observed_affinity'=r->>'expected_affinity' AND r->>'computed_affinity'=r->>'expected_affinity') THEN RAISE EXCEPTION 'GPU/NIC relations must originate from the independent Step 1 model comparison'; END IF;
  END LOOP;
 END LOOP;
 IF (SELECT count(DISTINCT gpu->>'component_id') FROM jsonb_array_elements(frozen->'bindings') b CROSS JOIN LATERAL jsonb_array_elements(b->'gpus') gpu)<>gpu_count THEN RAISE EXCEPTION 'every enabled GPU must be mapped'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mapping_assessment_scope BEFORE INSERT ON mapping_assessments FOR EACH ROW EXECUTE FUNCTION keel_mapping_scope();
CREATE TRIGGER mapping_revision_scope BEFORE INSERT ON mapping_revisions FOR EACH ROW EXECUTE FUNCTION keel_mapping_scope();

-- Historical publication projection. Consumers must recheck current G3
-- validity through mapping-svc; this view does not assert a current gate PASS.
CREATE VIEW mapping_published_ports AS
 SELECT latest.project_id,latest.asset_id,latest.machine_id,latest.id AS mapping_revision_id,latest.version,binding#>>'{plan,physical_port_id}' AS physical_port_id,binding
 FROM (SELECT DISTINCT ON(project_id,asset_id) * FROM mapping_revisions ORDER BY project_id,asset_id,version DESC) latest
 JOIN mapping_assessments assessment ON assessment.id=latest.assessment_id
 CROSS JOIN LATERAL jsonb_array_elements(assessment.candidate->'bindings') binding;
