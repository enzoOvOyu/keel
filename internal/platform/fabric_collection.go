package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"math"
	"strconv"
	"strings"
	"time"
)

type FabricDeviceSummary struct {
	ID           string          `json:"id"`
	Serial       string          `json:"serial"`
	Hostname     string          `json:"hostname"`
	Pod          string          `json:"pod"`
	Role         string          `json:"role"`
	Rack         string          `json:"rack"`
	Address      string          `json:"management_ip"`
	State        string          `json:"state"`
	Code         string          `json:"code,omitempty"`
	NOS          string          `json:"nos"`
	PortCount    int             `json:"port_count"`
	CollectedAt  *time.Time      `json:"collected_at,omitempty"`
	ContentHash  string          `json:"content_hash,omitempty"`
	Capabilities map[string]bool `json:"capabilities"`
}
type FabricCollectionReport struct {
	ID             string                `json:"id,omitempty"`
	PackageID      string                `json:"package_id"`
	SourceHash     string                `json:"source_hash"`
	EvidenceSource string                `json:"evidence_source"`
	Stage          string                `json:"stage"`
	NP3            string                `json:"np3"`
	Stale          bool                  `json:"stale"`
	CollectedAt    time.Time             `json:"collected_at"`
	ValidUntil     time.Time             `json:"valid_until"`
	Devices        []FabricDeviceSummary `json:"devices"`
	Findings       []fabricmon.Finding   `json:"findings"`
}
type FabricOptic struct {
	InUse   bool              `json:"in_use"`
	Port    string            `json:"port"`
	Data    *fabricmon.Optics `json:"data"`
	State   string            `json:"state"`
	Reasons []string          `json:"reasons"`
	Source  string            `json:"source"`
	RuleRef string            `json:"rule_ref"`
}
type FabricDeviceEvidence struct {
	RunID          string              `json:"run_id"`
	ContentHash    string              `json:"content_hash"`
	Stale          bool                `json:"stale"`
	EvidenceSource string              `json:"evidence_source"`
	Snapshot       *fabricmon.Snapshot `json:"snapshot"`
	Optics         []FabricOptic       `json:"optics"`
}

func fabricInventoryTx(ctx context.Context, tx *sql.Tx, project string) (string, []FabricDeviceSummary, []fabricmon.Cable, error) {
	var pkg string
	e := tx.QueryRowContext(ctx, `SELECT p.id FROM bom_import_packages p WHERE p.project_id=$1 AND p.state='committed' AND EXISTS(SELECT 1 FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=p.id AND b.category='switch') ORDER BY p.committed_at DESC,p.id DESC LIMIT 1`, project).Scan(&pkg)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil, nil, apiError(409, "FABRIC_BOM_REQUIRED", "A committed network BOM is required")
	}
	if e != nil {
		return "", nil, nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT b.item_uid,coalesce(b.serial,''),coalesce(a.hostname,s.hostname,''),coalesce(b.pod,''),coalesce(b.role,''),coalesce(b.position->>'rack',''),coalesce(host(a.management_ip),'') FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid JOIN switches s ON s.id=b.item_uid LEFT JOIN ztp_management_assignments a ON a.switch_id=b.item_uid AND a.project_id=$2 WHERE r.package_id=$1 AND b.category='switch' AND b.status<>'retired' ORDER BY b.item_uid`, pkg, project)
	if e != nil {
		return "", nil, nil, e
	}
	devices := []FabricDeviceSummary{}
	endpoints := map[string]fabricmon.Endpoint{}
	for rows.Next() {
		var d FabricDeviceSummary
		if e = rows.Scan(&d.ID, &d.Serial, &d.Hostname, &d.Pod, &d.Role, &d.Rack, &d.Address); e != nil {
			rows.Close()
			return "", nil, nil, e
		}
		d.State = "unknown"
		d.Code = "not_collected"
		d.Capabilities = map[string]bool{}
		devices = append(devices, d)
		endpoints[d.ID] = fabricmon.Endpoint{UID: d.ID, Switch: true, Serial: d.Serial, Hostname: d.Hostname}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return "", nil, nil, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT b.switch_id,b.mac FROM switch_mac_bindings b WHERE EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.package_id=$1 AND r.item_uid=b.switch_id)`, pkg)
	if e != nil {
		return "", nil, nil, e
	}
	for rows.Next() {
		var id, mac string
		if e = rows.Scan(&id, &mac); e != nil {
			break
		}
		d := endpoints[id]
		d.ChassisIDs = append(d.ChassisIDs, mac)
		endpoints[id] = d
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return "", nil, nil, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT c.item_uid,a.item_uid,b.item_uid,pa.category='switch',pb.category='switch',coalesce(pa.serial,''),coalesce(pb.serial,''),coalesce(nullif(a.port_name,''),a.port_index,''),coalesce(nullif(b.port_name,''),b.port_index,''),pa.item_uid,pb.item_uid FROM bom_item_revisions r JOIN bom_items c ON c.item_uid=r.item_uid JOIN bom_items a ON a.item_uid=c.endpoint_a_uid JOIN bom_items b ON b.item_uid=c.endpoint_b_uid JOIN bom_items pa ON pa.item_uid=a.parent_uid JOIN bom_items pb ON pb.item_uid=b.parent_uid WHERE r.package_id=$1 AND c.category='cable' AND c.status<>'retired' ORDER BY c.item_uid`, pkg)
	if e != nil {
		return "", nil, nil, e
	}
	cables := []fabricmon.Cable{}
	for rows.Next() {
		var c fabricmon.Cable
		var aid, bid string
		if e = rows.Scan(&c.UID, &c.A.UID, &c.B.UID, &c.A.Switch, &c.B.Switch, &c.A.Serial, &c.B.Serial, &c.A.Port, &c.B.Port, &aid, &bid); e != nil {
			break
		}
		c.A.ChassisIDs = endpoints[aid].ChassisIDs
		c.B.ChassisIDs = endpoints[bid].ChassisIDs
		cables = append(cables, c)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	return pkg, devices, cables, e
}
func (s *Store) CollectFabric(ctx context.Context, a *AuthSession, p *ProjectSummary, trace string) (*FabricCollectionReport, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:collect"); e != nil {
		tx.Rollback()
		return nil, e
	}
	pkg, devices, cables, e := fabricInventoryTx(ctx, tx, p.ID)
	tx.Rollback()
	if e != nil {
		return nil, e
	}
	if !s.FabricProbes.CollectorConfigured() {
		return nil, apiError(409, "FABRIC_COLLECTOR_NOT_CONFIGURED", "Configure a trusted mTLS fabric collector before reading switch observations")
	}
	raw, e := s.FabricProbes.ReadCollectorBatch(ctx, p.ID)
	if e != nil {
		return nil, apiError(502, "FABRIC_COLLECTOR_UNAVAILABLE", e.Error())
	}
	var batch struct {
		Version        string               `json:"version"`
		ProjectID      string               `json:"project_id"`
		EvidenceSource string               `json:"evidence_source"`
		Devices        []fabricmon.Snapshot `json:"devices"`
	}
	if json.Unmarshal(raw, &batch) != nil || batch.Version != "fabric-collection/v1" || batch.ProjectID != p.ID || len(batch.Devices) > 5000 || (batch.EvidenceSource != "device" && batch.EvidenceSource != "protocol_fixture") {
		return nil, apiError(502, "FABRIC_COLLECTOR_PROTOCOL_INVALID", "Collector response scope, version or provenance is invalid")
	}
	allowed := map[string]bool{}
	for _, d := range devices {
		allowed[d.Serial] = true
	}
	samples := map[string]fabricmon.Snapshot{}
	now := time.Now().UTC()
	totalPorts := 0
	for _, sample := range batch.Devices {
		if !allowed[sample.Serial] {
			return nil, apiError(502, "FABRIC_COLLECTOR_SCOPE_INVALID", "Collector returned a device outside the current project BOM")
		}
		if _, exists := samples[sample.Serial]; exists {
			return nil, apiError(502, "FABRIC_COLLECTOR_PROTOCOL_INVALID", "Duplicate switch sample")
		}
		if e = sample.Validate(sample.Serial, now); e != nil {
			return nil, apiError(502, "FABRIC_COLLECTOR_SAMPLE_INVALID", e.Error())
		}
		totalPorts += len(sample.Ports)
		if totalPorts > 100000 {
			return nil, apiError(502, "FABRIC_COLLECTOR_LIMIT", "Collection exceeds 100000 ports")
		}
		if sample.Capabilities == nil {
			sample.Capabilities = map[string]bool{}
		}
		if sample.Ports == nil {
			sample.Ports = []fabricmon.Port{}
		}
		samples[sample.Serial] = sample
	}
	out := &FabricCollectionReport{ID: RandomID(), PackageID: pkg, SourceHash: fabricboot.ManagementPlanDigest(string(raw)), EvidenceSource: batch.EvidenceSource, Stage: "N-S5", NP3: "BLOCKED", CollectedAt: now, ValidUntil: now.Add(5 * time.Minute), Devices: devices, Findings: []fabricmon.Finding{}}
	for i := range out.Devices {
		d := &out.Devices[i]
		sample, ok := samples[d.Serial]
		if !ok {
			d.Code = "collector_sample_missing"
			continue
		}
		d.State = "observed"
		d.Code = ""
		d.NOS = sample.NOS
		d.PortCount = len(sample.Ports)
		d.CollectedAt = &sample.CollectedAt
		d.Capabilities = sample.Capabilities
		d.ContentHash = fabricboot.ManagementPlanDigest(sample)
	}
	for _, sample := range samples {
		expiry := sample.CollectedAt.Add(5 * time.Minute)
		if expiry.Before(out.ValidUntil) {
			out.ValidUntil = expiry
		}
	}
	out.Findings = fabricmon.EvaluateCables(cables, samples)
	tx, e = s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:collect"); e != nil {
		return nil, e
	}
	current, _, _, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if current != pkg {
		return nil, apiError(409, "FABRIC_BOM_CHANGED", "BOM changed during collection; refresh observations")
	}
	encoded, _ := json.Marshal(out)
	_, e = tx.ExecContext(ctx, `INSERT INTO fabric_collection_runs(id,project_id,package_id,report,source_hash,collected_at,valid_until,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, out.ID, p.ID, pkg, encoded, out.SourceHash, out.CollectedAt, out.ValidUntil, a.User.ID)
	if e != nil {
		return nil, e
	}
	for _, d := range out.Devices {
		sample, ok := samples[d.Serial]
		if !ok {
			continue
		}
		encoded, _ = json.Marshal(sample)
		_, e = tx.ExecContext(ctx, `INSERT INTO fabric_switch_samples(run_id,switch_id,content_hash,sample) VALUES($1,$2,$3,$4)`, out.ID, d.ID, d.ContentHash, encoded)
		if e != nil {
			return nil, e
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.collection.read", out.ID, trace, map[string]any{"package_id": pkg, "source": out.EvidenceSource, "devices": len(samples), "np3": "BLOCKED"}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func latestFabricCollectionTx(ctx context.Context, tx *sql.Tx, project string) (*FabricCollectionReport, error) {
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT report FROM fabric_collection_runs WHERE project_id=$1 ORDER BY collected_at DESC,id DESC LIMIT 1`, project).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var out FabricCollectionReport
	if e = json.Unmarshal(raw, &out); e != nil {
		return nil, e
	}
	current, _, _, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	out.Stale = current != out.PackageID || time.Now().After(out.ValidUntil)
	if out.Stale {
		for i := range out.Findings {
			out.Findings[i].Verdict = "pending"
			out.Findings[i].Reason = "evidence_stale"
		}
	}
	return &out, nil
}
func (s *Store) LatestFabricCollection(ctx context.Context, a *AuthSession, p *ProjectSummary, trace string) (*FabricCollectionReport, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.collection.view", p.ID, trace, nil); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func fabricDDMLimitsTx(ctx context.Context, tx *sql.Tx, project string) (map[string]fabricmon.Range, error) {
	values, e := catalogDefaultsMatchingTx(ctx, tx, project, nil, "fabric.ddm.")
	if e != nil {
		return nil, e
	}
	out := map[string]fabricmon.Range{}
	for key, raw := range values {
		parts := strings.Split(strings.TrimPrefix(key, "fabric.ddm."), ".")
		if len(parts) != 2 {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, apiError(409, "FABRIC_DDM_LIMIT_INVALID", "DDM catalog threshold is not numeric")
		}
		r := out[parts[0]]
		switch parts[1] {
		case "warn_min":
			r.WarnMin = &v
		case "warn_max":
			r.WarnMax = &v
		case "critical_min":
			r.CriticalMin = &v
		case "critical_max":
			r.CriticalMax = &v
		default:
			continue
		}
		out[parts[0]] = r
	}
	for _, r := range out {
		if r.WarnMin != nil && r.WarnMax != nil && *r.WarnMin >= *r.WarnMax || r.CriticalMin != nil && r.CriticalMax != nil && *r.CriticalMin >= *r.CriticalMax || r.CriticalMin != nil && r.WarnMin != nil && *r.CriticalMin > *r.WarnMin || r.CriticalMax != nil && r.WarnMax != nil && *r.CriticalMax < *r.WarnMax {
			return nil, apiError(409, "FABRIC_DDM_LIMIT_INVALID", "DDM threshold ordering is invalid")
		}
	}
	return out, nil
}
func (s *Store) FabricDeviceEvidence(ctx context.Context, a *AuthSession, p *ProjectSummary, id, trace string) (*FabricDeviceEvidence, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	report, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if report == nil {
		return nil, ErrNotFound
	}
	var raw []byte
	out := &FabricDeviceEvidence{RunID: report.ID, Stale: report.Stale, EvidenceSource: report.EvidenceSource, Optics: []FabricOptic{}}
	e = tx.QueryRowContext(ctx, `SELECT content_hash,sample FROM fabric_switch_samples WHERE run_id=$1 AND switch_id=$2`, report.ID, id).Scan(&out.ContentHash, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &out.Snapshot); e != nil {
		return nil, e
	}
	limits, e := fabricDDMLimitsTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	pkg, devices, cables, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	serial := ""
	for _, d := range devices {
		if d.ID == id {
			serial = d.Serial
			break
		}
	}
	if serial == "" {
		return nil, ErrNotFound
	}
	names, e := modeledFabricPortsTx(ctx, tx, pkg, id)
	if e != nil {
		return nil, e
	}
	modeled := map[string]bool{}
	for _, name := range names {
		modeled[name] = true
	}
	used := usedFabricPorts(serial, cables)
	for _, port := range out.Snapshot.Ports {
		if !modeled[port.Name] {
			continue
		}
		state, reasons := fabricmon.JudgeOptics(port.Optics, limits)
		if !used[port.Name] {
			state = "unknown"
			reasons = []string{"unused_port_not_evaluated"}
		} else if !out.Snapshot.Capabilities["ddm"] {
			state = "unknown"
			reasons = []string{"ddm_capability_unavailable"}
		}
		if out.Stale {
			state = "unknown"
			reasons = []string{"evidence_stale"}
		}
		out.Optics = append(out.Optics, FabricOptic{InUse: used[port.Name], Port: port.Name, Data: port.Optics, State: state, Reasons: reasons, Source: "switch", RuleRef: "OPTICS-DDM@1"})
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.device.view", id, trace, map[string]any{"run_id": report.ID}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func (s *Store) FabricInventory(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]FabricDeviceSummary, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	_, devices, _, e := fabricInventoryTx(ctx, tx, p.ID)
	var domain *Error
	if errors.As(e, &domain) && domain.Code == "FABRIC_BOM_REQUIRED" {
		return []FabricDeviceSummary{}, nil
	}
	return devices, e
}
