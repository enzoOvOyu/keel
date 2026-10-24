package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricdelivery"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"sort"
	"time"
)

func deliveryStage(id, state, reason string) fabricdelivery.Stage {
	s := fabricdelivery.Stage{ID: id, State: state, Reasons: []string{}, EvidenceRefs: []string{}}
	if reason != "" {
		s.Reasons = append(s.Reasons, reason)
	}
	return s
}
func deliveryGate(stage string, g NetworkGate) fabricdelivery.Stage {
	return fabricdelivery.Stage{ID: stage, State: g.State, Reasons: append([]string{}, g.Reasons...), EvidenceRefs: []string{}}
}
func deliveryPayload(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func addDeliveryRef(f *fabricdelivery.Facts, kind, id, version, source string, payload any, at, until *time.Time) string {
	key := kind + ":" + id + "@" + version
	f.AddReference(fabricdelivery.Reference{ID: key, Kind: kind, ObjectID: id, Version: version, SHA256: fabricdelivery.Digest(payload), EvidenceSource: source, ObservedAt: at, ValidUntil: until})
	return key
}

type deliveryBOMRow struct {
	Object   fabricdelivery.Object
	Snapshot json.RawMessage
	Updated  time.Time
	Recorded time.Time
	Line     int64
}

func (s *Store) networkDeliveryFactsTx(ctx context.Context, tx *sql.Tx, project string) (*fabricdelivery.Facts, error) {
	now := time.Now().UTC()
	f := &fabricdelivery.Facts{ProjectID: project, Stages: []fabricdelivery.Stage{}, References: []fabricdelivery.Reference{}, Objects: []fabricdelivery.Object{}, Addresses: json.RawMessage(`[]`), ConfigurationIndex: json.RawMessage(`[]`), RuleVersions: []string{}, HardBlocks: []string{}, ValidUntil: now.Add(30 * time.Second)}
	var projectRaw []byte
	e := tx.QueryRowContext(ctx, `SELECT to_jsonb(p) FROM projects p WHERE p.id=$1`, project).Scan(&projectRaw)
	if e != nil {
		return nil, e
	}
	projectRef := addDeliveryRef(f, "project", project, "current", "project_registry", json.RawMessage(projectRaw), nil, nil)
	// No project-template application ledger exists yet; existence of a project or
	// an approved network template must not impersonate N-S0 standard adoption.
	ns0 := deliveryStage("N-S0", "BLOCKED", "project_template_application_evidence_not_available")
	ns0.EvidenceRefs = []string{projectRef}
	f.Stages = append(f.Stages, ns0)
	var pkgRaw []byte
	var profile string
	e = tx.QueryRowContext(ctx, `SELECT p.id,p.profile_version_ref,to_jsonb(p) FROM bom_import_packages p WHERE p.project_id=$1 AND p.state='committed' AND EXISTS(SELECT 1 FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=p.id AND b.category='switch') ORDER BY p.committed_at DESC,p.id DESC LIMIT 1`, project).Scan(&f.PackageID, &profile, &pkgRaw)
	if errors.Is(e, sql.ErrNoRows) {
		f.HardBlocks = append(f.HardBlocks, "committed_network_bom_required")
		return f, nil
	}
	if e != nil {
		return nil, e
	}
	packageRef := addDeliveryRef(f, "bom_package", f.PackageID, "bom-import/v1", "design_bom", json.RawMessage(pkgRaw), nil, nil)
	ns1 := deliveryStage("N-S1", "PASS", "")
	ns1.EvidenceRefs = []string{packageRef}
	var blocks int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bom_import_issues WHERE package_id=$1 AND severity='BLOCK'`, f.PackageID).Scan(&blocks); e != nil {
		return nil, e
	}
	if blocks > 0 {
		ns1.State = "BLOCKED"
		ns1.Reasons = append(ns1.Reasons, "bom_blocking_issues")
		f.HardBlocks = append(f.HardBlocks, "bom_blocking_issues")
	}
	var profileRaw []byte
	e = tx.QueryRowContext(ctx, `SELECT to_jsonb(p) FROM bom_mapping_profiles p WHERE p.project_id=$1 AND p.version_ref=$2 AND p.state='approved'`, project, profile).Scan(&profileRaw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e == nil {
		ns1.EvidenceRefs = append(ns1.EvidenceRefs, addDeliveryRef(f, "bom_mapping_profile", profile, profile, "approved_configuration", json.RawMessage(profileRaw), nil, nil))
	} else {
		ns1.State = "BLOCKED"
		ns1.Reasons = append(ns1.Reasons, "approved_bom_mapping_profile_required")
	}
	rows, e := tx.QueryContext(ctx, `SELECT b.item_uid,b.category,coalesce(b.serial,''),coalesce(b.parent_uid,''),coalesce(b.port_name,''),coalesce(b.port_index,''),coalesce(b.port_role,''),coalesce(b.model_version_id,''),coalesce(b.endpoint_a_uid,''),coalesce(b.endpoint_b_uid,''),coalesce(b.pod,''),coalesce(b.role,''),coalesce(b.rail,''),b.position,b.status,b.updated_at,r.row_snapshot,r.recorded_at,r.bom_line_no FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=$1 AND r.project_id=$2 ORDER BY b.item_uid`, f.PackageID, project)
	if e != nil {
		return nil, e
	}
	all := map[string]deliveryBOMRow{}
	for rows.Next() {
		var r deliveryBOMRow
		var pos, raw []byte
		o := &r.Object
		if e = rows.Scan(&o.ID, &o.Category, &o.Serial, &o.ParentID, &o.PortName, &o.PortIndex, &o.PortRole, &o.ModelVersion, &o.EndpointA, &o.EndpointB, &o.Pod, &o.Role, &o.Rail, &pos, &o.Status, &r.Updated, &raw, &r.Recorded, &r.Line); e != nil {
			rows.Close()
			return nil, e
		}
		o.Position = json.RawMessage(pos)
		r.Snapshot = json.RawMessage(raw)
		o.Source = "design_bom"
		o.State = "unknown"
		o.SampleRefs = []string{}
		all[o.ID] = r
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	selected := map[string]bool{}
	var include func(string)
	include = func(id string) {
		if id == "" || selected[id] {
			return
		}
		selected[id] = true
		r, ok := all[id]
		if !ok {
			f.HardBlocks = append(f.HardBlocks, "bom_reference_outside_snapshot:"+id)
			return
		}
		include(r.Object.ParentID)
		include(r.Object.EndpointA)
		include(r.Object.EndpointB)
	}
	for id, r := range all {
		if r.Object.Category == "switch" || r.Object.Category == "cable" || r.Object.Category == "optic_module" {
			include(id)
		}
		if r.Object.Category == "port" {
			if parent, ok := all[r.Object.ParentID]; ok && parent.Object.Category == "switch" {
				include(id)
			}
		}
	}
	for id := range selected {
		r, ok := all[id]
		if !ok {
			continue
		}
		o := r.Object
		o.BOMRef = addDeliveryRef(f, "bom_revision", f.PackageID+":"+o.ID, fmt.Sprint(r.Line), "design_bom", r.Snapshot, &r.Recorded, nil)
		o.AssetRef = addDeliveryRef(f, "asset_registry", o.ID, r.Updated.Format(time.RFC3339Nano), "asset_registry", o, &r.Updated, nil)
		f.Objects = append(f.Objects, o)
	}
	sort.Slice(f.Objects, func(i, j int) bool { return f.Objects[i].ID < f.Objects[j].ID })
	f.Stages = append(f.Stages, ns1)
	var referenceCount int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bom_item_revisions WHERE package_id=$1`, f.PackageID).Scan(&referenceCount); e != nil {
		return nil, e
	}
	if referenceCount > 100000 {
		return nil, apiError(409, "NETWORK_DELIVERY_SCOPE_LIMIT", "Use a streaming archive adapter above 100000 BOM objects")
	}
	readiness, e := s.networkReadinessTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	if readiness.ValidUntil.Before(f.ValidUntil) {
		f.ValidUntil = readiness.ValidUntil
	}
	collection, e := latestFabricCollectionTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	snapshots := map[string]fabricmon.Snapshot{}
	sampleRefs := map[string]string{}
	collectionRef := ""
	if collection != nil {
		var storedCollection []byte
		if e = tx.QueryRowContext(ctx, `SELECT report FROM fabric_collection_runs WHERE id=$1 AND project_id=$2`, collection.ID, project).Scan(&storedCollection); e != nil {
			return nil, e
		}
		collectionRef = addDeliveryRef(f, "fabric_collection", collection.ID, "fabric-collection/v1", collection.EvidenceSource, json.RawMessage(storedCollection), &collection.CollectedAt, &collection.ValidUntil)
		if collection.EvidenceSource == "protocol_fixture" {
			f.HardBlocks = append(f.HardBlocks, "protocol_fixture_is_not_production_delivery_evidence")
		}
		sampleRows, e := tx.QueryContext(ctx, `SELECT switch_id,content_hash,sample FROM fabric_switch_samples WHERE run_id=$1 ORDER BY switch_id`, collection.ID)
		if e != nil {
			return nil, e
		}
		for sampleRows.Next() {
			var id, hash string
			var raw []byte
			var sample fabricmon.Snapshot
			if e = sampleRows.Scan(&id, &hash, &raw); e != nil {
				sampleRows.Close()
				return nil, e
			}
			if e = json.Unmarshal(raw, &sample); e != nil {
				sampleRows.Close()
				return nil, e
			}
			if fabricboot.ManagementPlanDigest(sample) != hash {
				f.HardBlocks = append(f.HardBlocks, "switch_sample_digest_mismatch:"+id)
			}
			snapshots[id] = sample
			sampleRefs[id] = addDeliveryRef(f, "switch_sample", collection.ID+":"+id, "fabric-snapshot/v1", collection.EvidenceSource, sample, &sample.CollectedAt, &collection.ValidUntil)
		}
		e = sampleRows.Err()
		sampleRows.Close()
		if e != nil {
			return nil, e
		}
	}
	ns2 := deliveryStage("N-S2", "PASS", "")
	ns3 := deliveryStage("N-S3", "PASS", "")
	ns4 := deliveryStage("N-S4", "PASS", "")
	ns5 := deliveryStage("N-S5", "BLOCKED", "current_network_precheck_required")
	ns6 := deliveryStage("N-S6", "BLOCKED", "current_golden_configuration_required")
	if collection != nil && !collection.Stale {
		ns5 = deliveryStage("N-S5", "PASS", "")
		ns5.EvidenceRefs = []string{collectionRef}
		if collection.EvidenceSource != "device" {
			ns5.State = "BLOCKED"
			ns5.Reasons = append(ns5.Reasons, "device_evidence_required")
		}
		for _, finding := range collection.Findings {
			if finding.Verdict == "anomaly" {
				ns5.State = "FAIL"
				ns5.Reasons = append(ns5.Reasons, finding.IssueType)
			} else if finding.Verdict != "precheck_pass" {
				if finding.Reason == "server_side_pending" {
					ns5.Reasons = append(ns5.Reasons, "server_G2_confirmation_pending_not_claimed_as_NP3")
				} else {
					ns5.State = "BLOCKED"
					ns5.Reasons = append(ns5.Reasons, finding.Reason)
				}
			}
		}
	}
	batches := map[string]bool{}
	assigned := map[string]bool{}
	addresses := []any{}
	ar, e := tx.QueryContext(ctx, `SELECT switch_id,serial_number,hostname,host(management_ip),host(gateway),batch_ref,plan_id FROM ztp_management_assignments WHERE project_id=$1 ORDER BY switch_id`, project)
	if e != nil {
		return nil, e
	}
	for ar.Next() {
		var id, sn, hostname, ip, gateway, batch, plan string
		if e = ar.Scan(&id, &sn, &hostname, &ip, &gateway, &batch, &plan); e != nil {
			ar.Close()
			return nil, e
		}
		if !selected[id] {
			continue
		}
		batches[batch] = true
		assigned[id] = true
		addresses = append(addresses, map[string]string{"switch_id": id, "serial_number": sn, "hostname": hostname, "management_ip": ip, "gateway": gateway, "batch_ref": batch, "plan_id": plan})
	}
	e = ar.Err()
	ar.Close()
	if e != nil {
		return nil, e
	}
	f.Addresses = deliveryPayload(addresses)
	if len(batches) == 0 {
		ns2.State = "BLOCKED"
		ns2.Reasons = append(ns2.Reasons, "approved_management_plan_required")
		ns3.State = "BLOCKED"
		ns3.Reasons = append(ns3.Reasons, "environment_checks_required")
	}
	for batch := range batches {
		plan, err := currentManagementPlanTx(ctx, tx, project, batch)
		if err != nil {
			var business *Error
			if !errors.As(err, &business) {
				return nil, err
			}
			ns2.State = "BLOCKED"
			ns2.Reasons = append(ns2.Reasons, "management_plan_stale")
			ns3.State = "BLOCKED"
			ns3.Reasons = append(ns3.Reasons, "management_plan_stale")
			continue
		}
		ref := addDeliveryRef(f, "management_plan", plan.ID, plan.ContentHash, "approved_configuration", plan, plan.CreatedAt, nil)
		ns2.EvidenceRefs = append(ns2.EvidenceRefs, ref)
		var baselineRaw []byte
		if e = tx.QueryRowContext(ctx, `SELECT to_jsonb(b) FROM network_baseline_versions b WHERE id=$1 AND project_id=$2 AND state='approved'`, plan.BaselineID, project).Scan(&baselineRaw); e != nil {
			return nil, e
		}
		ns2.EvidenceRefs = append(ns2.EvidenceRefs, addDeliveryRef(f, "network_baseline", plan.BaselineID, plan.BaselineRef, "approved_configuration", json.RawMessage(baselineRaw), nil, nil))
		var raw []byte
		e = tx.QueryRowContext(ctx, `SELECT report FROM ztp_environment_checks WHERE project_id=$1 AND batch_ref=$2 AND plan_id=$3 AND boot_channel='239' ORDER BY checked_at DESC,id DESC LIMIT 1`, project, batch, plan.ID).Scan(&raw)
		if errors.Is(e, sql.ErrNoRows) {
			ns3.State = "BLOCKED"
			ns3.Reasons = append(ns3.Reasons, "environment_checks_required")
			continue
		}
		if e != nil {
			return nil, e
		}
		var env ZTPEnvironmentReport
		if e = json.Unmarshal(raw, &env); e != nil {
			return nil, e
		}
		ns3.EvidenceRefs = append(ns3.EvidenceRefs, addDeliveryRef(f, "ztp_environment", env.ID, "N-S3", "control_plane_probe", env, &env.CheckedAt, &env.ExpiresAt))
		if !env.Ready || !env.ExpiresAt.After(now) || s.FabricProbes == nil || env.ConfigHash != s.FabricProbes.Fingerprint {
			ns3.State = "BLOCKED"
			ns3.Reasons = append(ns3.Reasons, "environment_checks_stale_or_failed")
		}
	}
	configIndex := []any{}
	modelRefs := map[string]string{}
	activeSwitches := 0
	for i := range f.Objects {
		o := &f.Objects[i]
		if o.ModelVersion != "" {
			if ref, ok := modelRefs[o.ModelVersion]; ok {
				o.ModelRef = ref
			} else {
				var raw []byte
				e = tx.QueryRowContext(ctx, `SELECT to_jsonb(m) FROM device_model_versions m WHERE id=$1`, o.ModelVersion).Scan(&raw)
				if e != nil {
					return nil, e
				}
				o.ModelRef = addDeliveryRef(f, "device_model_version", o.ModelVersion, o.ModelVersion, "approved_configuration", json.RawMessage(raw), nil, nil)
				modelRefs[o.ModelVersion] = o.ModelRef
			}
		}
		if o.Status == "retired" {
			o.State = "retired"
			continue
		}
		if o.Category == "switch" {
			activeSwitches++
			if !assigned[o.ID] {
				ns2.State = "BLOCKED"
				ns2.Reasons = append(ns2.Reasons, "management_assignment_missing:"+o.ID)
			}
			var raw []byte
			var sid, state string
			var updated time.Time
			e = tx.QueryRowContext(ctx, `SELECT id,state,updated_at,to_jsonb(z) FROM ztp_sessions z WHERE project_id=$1 AND switch_id=$2 AND mode='device' ORDER BY updated_at DESC,id DESC LIMIT 1`, project, o.ID).Scan(&sid, &state, &updated, &raw)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if e == nil {
				ns4.EvidenceRefs = append(ns4.EvidenceRefs, addDeliveryRef(f, "ztp_session", sid, "device-nine-stage", "device", json.RawMessage(raw), &updated, nil))
			}
			if state != "managed" {
				ns4.State = "BLOCKED"
				ns4.Reasons = append(ns4.Reasons, "authenticated_ztp_manageability_required:"+o.ID)
			}
			if o.ModelVersion == "" {
				ns1.State = "BLOCKED"
				ns1.Reasons = append(ns1.Reasons, "switch_model_version_required:"+o.ID)
			}
			if sample, ok := snapshots[o.ID]; ok {
				o.SampleRefs = []string{sampleRefs[o.ID]}
				o.Source = "switch"
				o.EvidenceSource = collection.EvidenceSource
				o.State = "observed"
				o.Details = deliveryPayload(map[string]any{"hostname_source": "approved_management_plan", "nos": sample.NOS, "bgp_up": sample.BGPUp, "ptp_locked": sample.PTPLocked, "capabilities": sample.Capabilities, "ztp_state": state, "collected_at": sample.CollectedAt, "source": "switch"})
			}
			var jobID, intendedID, pushState string
			e = tx.QueryRowContext(ctx, `SELECT j.id,d.intended_id,d.state FROM network_push_jobs j JOIN network_push_devices d ON d.job_id=j.id WHERE j.project_id=$1 AND d.switch_id=$2 AND j.push_execution_id IS NOT NULL ORDER BY j.created_at DESC,j.id DESC LIMIT 1`, project, o.ID).Scan(&jobID, &intendedID, &pushState)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if e == nil {
				j, err := loadNetworkPush(ctx, tx, project, jobID, false)
				if err != nil {
					return nil, err
				}
				jref := addDeliveryRef(f, "network_push", j.ID, j.ReviewHash, "approved_configuration", j, &j.CreatedAt, nil)
				var intendedRaw []byte
				var intendedHash, templateID string
				if e = tx.QueryRowContext(ctx, `SELECT configuration,content_hash,template_id FROM network_intended_configs WHERE id=$1 AND project_id=$2 AND switch_id=$3`, intendedID, project, o.ID).Scan(&intendedRaw, &intendedHash, &templateID); e != nil {
					return nil, e
				}
				iref := addDeliveryRef(f, "intended_configuration", intendedID, intendedHash, "approved_configuration", json.RawMessage(intendedRaw), nil, nil)
				var templateRaw []byte
				var templateVersion int64
				if e = tx.QueryRowContext(ctx, `SELECT to_jsonb(t),version FROM network_template_versions t WHERE id=$1 AND project_id=$2`, templateID, project).Scan(&templateRaw, &templateVersion); e != nil {
					return nil, e
				}
				tref := addDeliveryRef(f, "network_template", templateID, fmt.Sprint(templateVersion), "approved_configuration", json.RawMessage(templateRaw), nil, nil)
				for _, d := range j.Devices {
					if d.Device.ID == o.ID {
						entry := map[string]any{"switch_id": o.ID, "push_job_ref": jref, "template_ref": tref, "intended_ref": iref, "running_hash": d.RunningHash, "snapshot_ref": d.SnapshotRef, "state": pushState, "diff": d.Diff, "code": d.Code}
						if d.Verify != nil {
							version := d.Verify.RunningHash
							if version == "" {
								version = "unavailable"
							}
							vref := addDeliveryRef(f, "running_configuration", jobID+":"+o.ID, version, d.Verify.EvidenceSource, d.Verify, &d.Verify.ObservedAt, nil)
							entry["running_ref"] = vref
							if d.Verify.EvidenceSource == "protocol_fixture" {
								f.HardBlocks = append(f.HardBlocks, "protocol_fixture_running_configuration:"+o.ID)
							}
						}
						configIndex = append(configIndex, entry)
						break
					}
				}
			}
		} else if o.Category == "port" || o.Category == "optic_module" {
			parent := o.ParentID
			if p, ok := all[parent]; ok && p.Object.Category == "port" {
				parent = p.Object.ParentID
			}
			if sample, ok := snapshots[parent]; ok {
				if sampleRefs[parent] != "" {
					o.SampleRefs = []string{sampleRefs[parent]}
				}
				portName := o.PortName
				if o.Category == "optic_module" {
					portName = all[o.ParentID].Object.PortName
				}
				for _, port := range sample.Ports {
					if port.Name == portName {
						o.Details = deliveryPayload(map[string]any{"interface": port, "source": "switch", "collected_at": sample.CollectedAt})
						o.Source = "switch"
						o.EvidenceSource = collection.EvidenceSource
						o.State = "observed"
						break
					}
				}
			}
		}
		if o.Category == "cable" && collection != nil {
			for _, finding := range collection.Findings {
				if finding.CableUID == o.ID {
					o.State = finding.Verdict
					o.Source = "switch"
					o.EvidenceSource = collection.EvidenceSource
					o.SampleRefs = []string{collectionRef}
					o.Details = deliveryPayload(finding)
					break
				}
			}
		}
	}
	if activeSwitches == 0 {
		ns4 = deliveryStage("N-S4", "BLOCKED", "active_switch_inventory_required")
	}
	f.ConfigurationIndex = deliveryPayload(configIndex)
	// N-S6 reuses the same Golden/authenticated ZTP/current-facts predicate as NP-2.
	for _, gate := range readiness.Gates {
		if gate.ID == "NP-2" {
			ns6 = deliveryGate("N-S6", gate)
			for _, entry := range f.References {
				if entry.Kind == "network_push" || entry.Kind == "intended_configuration" || entry.Kind == "running_configuration" {
					ns6.EvidenceRefs = append(ns6.EvidenceRefs, entry.ID)
				}
			}
		}
	}
	ns7 := deliveryGate("N-S7", readiness.Health.NS7)
	for i := range f.Objects {
		for _, d := range readiness.Health.Devices {
			if f.Objects[i].Category == "switch" && f.Objects[i].ID == d.Device.ID {
				f.Objects[i].Details = deliveryPayload(map[string]any{"source": "switch", "protocol": d.Protocol, "optics": d.Optics, "lossless": d.Lossless.Fabric, "finding_counts": d.FindingCounts, "applied_rule_ids": d.AppliedRuleIDs, "ztp_details": f.Objects[i].Details})
			}
		}
	}
	for _, gate := range readiness.Gates {
		if gate.ID == "NP-6" && gate.State != "PASS" {
			ns7.State = "BLOCKED"
			ns7.Reasons = append(ns7.Reasons, gate.Reasons...)
		}
	}
	if collectionRef != "" {
		ns7.EvidenceRefs = append(ns7.EvidenceRefs, collectionRef)
	}
	ruleRows, e := tx.QueryContext(ctx, `SELECT id,rule_id,version,state,content_hash,definition FROM network_rule_versions WHERE project_id=$1 AND state IN('released','disabled') ORDER BY rule_id,version`, project)
	if e != nil {
		return nil, e
	}
	for ruleRows.Next() {
		var id, name, state, hash string
		var version int64
		var raw []byte
		if e = ruleRows.Scan(&id, &name, &version, &state, &hash, &raw); e != nil {
			ruleRows.Close()
			return nil, e
		}
		ref := fmt.Sprintf("%s@%d", name, version)
		ns7.EvidenceRefs = append(ns7.EvidenceRefs, addDeliveryRef(f, "network_rule", id, ref, "approved_configuration", json.RawMessage(raw), nil, nil))
	}
	e = ruleRows.Err()
	ruleRows.Close()
	if e != nil {
		return nil, e
	}
	for _, device := range readiness.Health.Devices {
		f.RuleVersions = append(f.RuleVersions, device.AppliedRuleIDs...)
	}
	f.RuleVersions = uniqueDeliveryStrings(f.RuleVersions)
	for _, o := range f.Objects {
		if o.Category == "cable" && len(o.Details) > 0 {
			var finding fabricmon.Finding
			if json.Unmarshal(o.Details, &finding) == nil && finding.RuleRef != "" {
				f.RuleVersions = append(f.RuleVersions, finding.RuleRef)
			}
		}
	}
	f.RuleVersions = uniqueDeliveryStrings(f.RuleVersions)
	sort.Strings(f.RuleVersions)

	catalog, e := fabricmon.DefaultLosslessCatalog()
	if e != nil {
		return nil, e
	}
	catalogRef := addDeliveryRef(f, "builtin_network_catalog", "lossless-defaults", "v1", "builtin_rules", catalog, nil, nil)
	ns7.EvidenceRefs = append(ns7.EvidenceRefs, catalogRef)
	policyRows, e := tx.QueryContext(ctx, `SELECT p.key,p.scope_key,to_jsonb(p) FROM param_keys p WHERE p.state='active' AND p.key LIKE 'fabric.%' AND p.scope_key IN('global',$1) ORDER BY p.scope_key,p.key`, "project:"+project)
	if e != nil {
		return nil, e
	}
	for policyRows.Next() {
		var key, scope string
		var raw []byte
		if e = policyRows.Scan(&key, &scope, &raw); e != nil {
			policyRows.Close()
			return nil, e
		}
		ref := addDeliveryRef(f, "parameter_policy", scope+":"+key, fabricdelivery.Digest(json.RawMessage(raw)), "parameter_catalog", json.RawMessage(raw), nil, nil)
		ns7.EvidenceRefs = append(ns7.EvidenceRefs, ref)
	}
	e = policyRows.Err()
	policyRows.Close()
	if e != nil {
		return nil, e
	}
	for i := range f.Stages {
		if f.Stages[i].ID == "N-S1" {
			f.Stages[i] = ns1
		}
	}
	f.Stages = append(f.Stages, ns2, ns3, ns4, ns5, ns6, ns7)
	// The live resource quarantine is an integrity blocker, never a waiver target.
	var quarantined bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_resource_leases l WHERE (l.state='reconciling' OR (l.state='held' AND l.mode='exclusive')) AND l.resource_id IN(SELECT 'ssh:switch:'||r.item_uid FROM bom_item_revisions r WHERE r.package_id=$1))`, f.PackageID).Scan(&quarantined)
	if e != nil {
		return nil, e
	}
	if quarantined {
		f.HardBlocks = append(f.HardBlocks, "network_unknown_write_resource_quarantine")
	}

	var identityConflict bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_identity_issues WHERE project_id=$1 AND state='open' AND boot_channel<>'sim')`, project).Scan(&identityConflict)
	if e != nil {
		return nil, e
	}
	if identityConflict {
		f.HardBlocks = append(f.HardBlocks, "unresolved_device_identity_conflict")
	}
	var controllerBusy bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_kea_deployments WHERE project_id=$1 AND state IN('applying','unknown'))`, project).Scan(&controllerBusy)
	if e != nil {
		return nil, e
	}
	if controllerBusy {
		f.HardBlocks = append(f.HardBlocks, "kea_write_in_progress_or_unknown")
	}
	if len(f.Objects) > 100000 {
		return nil, apiError(409, "NETWORK_DELIVERY_SCOPE_LIMIT", "This scope requires a streaming exporter; the inline reference graph is bounded to 100000 objects")
	}
	for i := range f.Stages {
		f.Stages[i].Reasons = uniqueDeliveryStrings(f.Stages[i].Reasons)
		f.Stages[i].EvidenceRefs = uniqueDeliveryStrings(f.Stages[i].EvidenceRefs)
	}
	f.HardBlocks = uniqueDeliveryStrings(f.HardBlocks)
	sort.Slice(f.References, func(i, j int) bool { return f.References[i].ID < f.References[j].ID })
	return f, nil
}
func uniqueDeliveryStrings(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
