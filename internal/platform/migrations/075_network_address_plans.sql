CREATE TABLE network_address_plans (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 baseline_id varchar(32) NOT NULL REFERENCES network_baseline_versions(id),
 management_plan_id varchar(32) NOT NULL REFERENCES ztp_management_plans(id),
 package_id varchar(32) NOT NULL REFERENCES bom_import_packages(id),
 input jsonb NOT NULL,
 report jsonb NOT NULL,
 content_hash varchar(64) NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(project_id,content_hash)
);
CREATE INDEX network_address_plans_recent ON network_address_plans(project_id,created_at DESC,id DESC);
CREATE TABLE network_address_plan_approvals (
 plan_id varchar(32) PRIMARY KEY REFERENCES network_address_plans(id),
 approved_by varchar(32) NOT NULL REFERENCES users(id),
 authority varchar(8) NOT NULL CHECK(authority IN('Admin','NH')),
 content_hash varchar(64) NOT NULL,
 reason varchar(1000) NOT NULL CHECK(length(trim(reason))>0),
 approved_at timestamptz NOT NULL
);
CREATE TRIGGER network_address_plan_immutable BEFORE UPDATE OR DELETE ON network_address_plans FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER network_address_approval_immutable BEFORE UPDATE OR DELETE ON network_address_plan_approvals FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE FUNCTION keel_network_address_approval_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE plan network_address_plans%ROWTYPE; reviewer users%ROWTYPE; reviewer_role varchar;
BEGIN
 SELECT * INTO STRICT plan FROM network_address_plans WHERE id=NEW.plan_id;
 SELECT * INTO STRICT reviewer FROM users WHERE id=NEW.approved_by;
 IF reviewer.id=plan.created_by OR NOT reviewer.enabled OR NEW.content_hash<>plan.content_hash THEN RAISE EXCEPTION 'Address planning requires independent enabled review of the exact plan'; END IF;
 IF reviewer.is_admin THEN
  IF NEW.authority<>'Admin' THEN RAISE EXCEPTION 'Address plan authority mismatch'; END IF;
 ELSE
  SELECT m.role INTO reviewer_role FROM memberships m WHERE m.project_id=plan.project_id AND m.user_id=reviewer.id;
  IF reviewer_role IS DISTINCT FROM 'NH' OR NEW.authority<>'NH' THEN RAISE EXCEPTION 'Address plan review requires current project NH authority'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER network_address_approval_scope BEFORE INSERT ON network_address_plan_approvals FOR EACH ROW EXECUTE FUNCTION keel_network_address_approval_scope();
CREATE FUNCTION keel_network_address_plan_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM network_baseline_versions b WHERE b.id=NEW.baseline_id AND b.project_id=NEW.project_id AND b.state='approved') OR NOT EXISTS(SELECT 1 FROM ztp_management_plans m WHERE m.id=NEW.management_plan_id AND m.project_id=NEW.project_id AND m.baseline_id=NEW.baseline_id) OR NOT EXISTS(SELECT 1 FROM bom_import_packages p WHERE p.id=NEW.package_id AND p.project_id=NEW.project_id AND p.state='committed') OR NEW.report->>'project_id' IS DISTINCT FROM NEW.project_id OR NEW.report->>'baseline_id' IS DISTINCT FROM NEW.baseline_id OR NEW.report->>'management_plan_id' IS DISTINCT FROM NEW.management_plan_id OR NEW.report->>'package_id' IS DISTINCT FROM NEW.package_id OR NEW.report->>'content_hash' IS DISTINCT FROM NEW.content_hash THEN RAISE EXCEPTION 'Address plan must bind one approved project baseline, management plan and BOM snapshot'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER network_address_plan_scope BEFORE INSERT ON network_address_plans FOR EACH ROW EXECUTE FUNCTION keel_network_address_plan_scope();
