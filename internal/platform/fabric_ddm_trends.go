package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"sort"
	"strconv"
	"time"
)

type FabricDDMLimit struct {
	WarnMin     *float64 `json:"warn_min"`
	WarnMax     *float64 `json:"warn_max"`
	CriticalMin *float64 `json:"critical_min"`
	CriticalMax *float64 `json:"critical_max"`
}
type FabricDDMPoint struct {
	ContinuousFromPrevious bool              `json:"continuous_from_previous"`
	At                     time.Time         `json:"at"`
	Port                   string            `json:"port"`
	ModuleSerial           string            `json:"module_serial"`
	Provider               string            `json:"provider"`
	EvidenceRef            string            `json:"evidence_ref"`
	Data                   *fabricmon.Optics `json:"data"`
	State                  string            `json:"state"`
	Reasons                []string          `json:"reasons"`
}
type FabricDDMTrends struct {
	RawPointCount         int                       `json:"raw_point_count"`
	Version               string                    `json:"version"`
	ProjectID             string                    `json:"project_id"`
	PackageID             string                    `json:"package_id"`
	DeviceID              string                    `json:"device_id"`
	Port                  string                    `json:"port"`
	Hours                 int                       `json:"hours"`
	Source                string                    `json:"source"`
	EvidenceSource        string                    `json:"evidence_source"`
	WindowStart           time.Time                 `json:"window_start"`
	WindowEnd             time.Time                 `json:"window_end"`
	InUse                 bool                      `json:"in_use"`
	SampleIntervalSeconds *float64                  `json:"sample_interval_seconds"`
	Thresholds            map[string]FabricDDMLimit `json:"thresholds"`
	ThresholdPolicyHash   string                    `json:"threshold_policy_hash"`
	ContentHash           string                    `json:"content_hash"`
	Points                []FabricDDMPoint          `json:"points"`
	Warnings              []string                  `json:"warnings"`
	ExportRef             string                    `json:"export_ref,omitempty"`
}

func fabricDDMIntervalTx(ctx context.Context, tx *sql.Tx, project string) (*float64, error) {
	rows, e := tx.QueryContext(ctx, `SELECT default_value FROM param_keys WHERE state='active' AND key='fabric.ddm_interval' AND scope_key IN('global',$1) ORDER BY CASE WHEN scope_key='global' THEN 0 ELSE 1 END`, "project:"+project)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var interval *float64
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		duration, e := time.ParseDuration(raw)
		if e != nil {
			seconds, parse := strconv.ParseFloat(raw, 64)
			if parse != nil || seconds <= 0 || seconds > 3600 {
				return nil, apiError(409, "FABRIC_DDM_INTERVAL_INVALID", "DDM interval must be a positive duration or seconds through3600")
			}
			duration = time.Duration(seconds * float64(time.Second))
		}
		if duration <= 0 || duration > time.Hour {
			return nil, apiError(409, "FABRIC_DDM_INTERVAL_INVALID", "DDM interval must be positive and at most one hour")
		}
		seconds := duration.Seconds()
		interval = &seconds
	}
	return interval, rows.Err()
}
func (s *Store) FabricDDMTrends(ctx context.Context, a *AuthSession, p *ProjectSummary, id, port string, hours int, exportHash, trace string) (*FabricDDMTrends, error) {
	if hours != 24 && hours != 48 || port == "" || len(port) > 128 {
		return nil, apiError(400, "FABRIC_DDM_QUERY_INVALID", "Choose a modeled port and a24/48hour window")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	pkg, devices, cables, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	var device *FabricDeviceSummary
	for i := range devices {
		if devices[i].ID == id {
			device = &devices[i]
			break
		}
	}
	if device == nil {
		return nil, ErrNotFound
	}
	ports, e := modeledFabricPortsTx(ctx, tx, pkg, id)
	if e != nil {
		return nil, e
	}
	found := false
	for _, name := range ports {
		if port == name {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	limits, e := fabricDDMLimitsTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	interval, e := fabricDDMIntervalTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	out := &FabricDDMTrends{Version: "fabric-ddm-trends/v1", ProjectID: p.ID, PackageID: pkg, DeviceID: id, Port: port, Hours: hours, Source: "switch", WindowEnd: now, WindowStart: now.Add(-time.Duration(hours) * time.Hour), InUse: usedFabricPorts(device.Serial, cables)[port], SampleIntervalSeconds: interval, Thresholds: map[string]FabricDDMLimit{}, ThresholdPolicyHash: netbaseline.ContentHash(limits), Points: []FabricDDMPoint{}, Warnings: []string{}}
	for key, limit := range limits {
		out.Thresholds[key] = FabricDDMLimit{limit.WarnMin, limit.WarnMax, limit.CriticalMin, limit.CriticalMax}
	}
	report, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if report != nil {
		out.EvidenceSource = report.EvidenceSource
	}
	rows, e := tx.QueryContext(ctx, `SELECT s.content_hash,jsonb_build_object('provider',s.sample->'provider','collected_at',s.sample->'collected_at','capabilities',jsonb_build_object('ddm',s.sample->'capabilities'->'ddm'),'ports',jsonb_build_array(observed_port.value)) FROM fabric_switch_samples s JOIN fabric_collection_runs r ON r.id=s.run_id CROSS JOIN LATERAL (SELECT value FROM jsonb_array_elements(CASE WHEN jsonb_typeof(s.sample->'ports')='array' THEN s.sample->'ports' ELSE '[]'::jsonb END) p(value) WHERE value->>'name'=$6 LIMIT 1) observed_port WHERE r.project_id=$1 AND r.package_id=$2 AND s.switch_id=$3 AND r.collected_at>=$4 AND r.report->>'evidence_source'=$5 ORDER BY r.collected_at,s.content_hash LIMIT 17282`, p.ID, pkg, id, out.WindowStart, out.EvidenceSource, port)
	if e != nil {
		return nil, e
	}
	count := 0
	seen := map[string]string{}
	conflict := false
	for rows.Next() {
		count++
		if count > 17281 {
			rows.Close()
			return nil, apiError(413, "FABRIC_DDM_HISTORY_LIMIT", "Requested device history exceeds bounded query capacity")
		}
		var hash string
		var raw []byte
		var sample fabricmon.Snapshot
		if e = rows.Scan(&hash, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(raw, &sample); e != nil {
			rows.Close()
			return nil, e
		}
		if sample.CollectedAt.Before(out.WindowStart) || sample.CollectedAt.After(now) {
			continue
		}
		for _, observed := range sample.Ports {
			if observed.Name != port {
				continue
			}
			key := sample.CollectedAt.UTC().Format(time.RFC3339Nano)
			encoded := netbaseline.ContentHash([]any{sample.Provider, observed.Optics, sample.Capabilities["ddm"], observed.OpticsApplicable})
			if old, ok := seen[key]; ok {
				conflict = conflict || old != encoded
				continue
			}
			seen[key] = encoded
			state, reasons := fabricmon.JudgeOptics(observed.Optics, limits)
			if !out.InUse {
				state = "unknown"
				reasons = []string{"unused_port_not_evaluated"}
			} else if !sample.Capabilities["ddm"] {
				state = "unknown"
				reasons = []string{"ddm_capability_unavailable"}
			}
			module := ""
			if observed.Optics != nil {
				module = observed.Optics.Serial
			}
			out.Points = append(out.Points, FabricDDMPoint{At: sample.CollectedAt, Port: port, ModuleSerial: module, Provider: sample.Provider, EvidenceRef: hash, Data: observed.Optics, State: state, Reasons: reasons})
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].At.Before(out.Points[j].At) })
	if conflict {
		out.Warnings = append(out.Warnings, "conflicting_same_timestamp_observations")
		for i := range out.Points {
			out.Points[i].State = "unknown"
			out.Points[i].Reasons = append(out.Points[i].Reasons, "conflicting_same_timestamp_observations")
		}
	}
	if len(limits) == 0 {
		out.Warnings = append(out.Warnings, "ddm_thresholds_unknown")
	}
	if interval == nil {
		out.Warnings = append(out.Warnings, "ddm_sampling_interval_unknown")
	}
	if len(out.Points) == 0 {
		out.Warnings = append(out.Warnings, "ddm_history_empty")
	} else if now.Sub(out.Points[len(out.Points)-1].At) > 5*time.Minute {
		out.Warnings = append(out.Warnings, "latest_ddm_observation_stale")
	}
	if !out.InUse {
		out.Warnings = append(out.Warnings, "unused_port_not_evaluated")
	}
	// Thresholds are the current parameter/template policy, not a rewrite of the
	// immutable archived judgement. The exported pixels are rendered client-side.
	out.Warnings = append(out.Warnings, "threshold_lines_use_current_policy")
	if out.SampleIntervalSeconds != nil && len(out.Points) > 0 {
		interval := time.Duration(*out.SampleIntervalSeconds * float64(time.Second))
		incomplete := out.Points[0].At.Sub(out.WindowStart) > interval*3/2 || now.Sub(out.Points[len(out.Points)-1].At) > interval*3/2
		for i := 1; i < len(out.Points); i++ {
			if out.Points[i].At.Sub(out.Points[i-1].At) > interval*3/2 {
				incomplete = true
				break
			}
		}
		if incomplete {
			out.Warnings = append(out.Warnings, "ddm_window_coverage_incomplete")
		}
	}
	out.RawPointCount = len(out.Points)
	historyHash := netbaseline.ContentHash(out.Points)
	out.Points = boundedDDMPoints(out.Points, out.SampleIntervalSeconds)
	if len(out.Points) < out.RawPointCount {
		out.Warnings = append(out.Warnings, "ddm_history_downsampled_extrema")
	}
	out.ContentHash = netbaseline.ContentHash([]any{out.ProjectID, out.PackageID, out.DeviceID, out.Port, out.Hours, out.EvidenceSource, out.InUse, out.SampleIntervalSeconds, out.ThresholdPolicyHash, historyHash, out.Points, out.Warnings})
	action := "fabric.ddm.trends.read"
	if exportHash != "" {
		if exportHash != out.ContentHash {
			return nil, apiError(409, "FABRIC_DDM_EXPORT_CHANGED", "History or current thresholds changed; refresh the chart before export")
		}
		action = "fabric.ddm.trends.export"
		out.ExportRef = RandomID()
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, action, id, trace, map[string]any{"port": port, "hours": hours, "content_hash": out.ContentHash, "threshold_policy_hash": out.ThresholdPolicyHash, "sample_count": len(out.Points), "export_ref": out.ExportRef}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// boundedDDMPoints retains original timestamped evidence, including each metric's
// extrema. Continuity is checked over every raw sample, not inferred from the
// larger spacing between retained points. Missing data or a source/module change
// therefore remains a break even when its sample is not selected for display.
func boundedDDMPoints(raw []FabricDDMPoint, interval *float64) []FabricDDMPoint {
	const maxPoints = 2000
	values := func(p FabricDDMPoint) [5]*float64 {
		if p.Data == nil {
			return [5]*float64{}
		}
		return [5]*float64{p.Data.RxDBM, p.Data.TxDBM, p.Data.TemperatureC, p.Data.VoltageV, p.Data.BiasMA}
	}
	valid := func(p FabricDDMPoint) bool {
		if p.ModuleSerial == "" || p.Provider == "" {
			return false
		}
		for _, r := range p.Reasons {
			if r == "ddm_capability_unavailable" || r == "conflicting_same_timestamp_observations" {
				return false
			}
		}
		for _, v := range values(p) {
			if v == nil {
				return false
			}
		}
		return true
	}
	breaks := make([]int, len(raw))
	for i := 1; i < len(raw); i++ {
		breaks[i] = breaks[i-1]
		elapsed := raw[i].At.Sub(raw[i-1].At).Seconds()
		if interval == nil || !valid(raw[i-1]) || !valid(raw[i]) || raw[i].ModuleSerial != raw[i-1].ModuleSerial || raw[i].Provider != raw[i-1].Provider || elapsed <= 0 || elapsed > *interval*1.5 {
			breaks[i]++
		}
	}
	selected := map[int]bool{}
	if len(raw) <= maxPoints {
		for i := range raw {
			selected[i] = true
		}
	} else {
		// Five min/max pairs plus first/last cost at most twelve points per bucket.
		buckets := maxPoints / 12
		width := (len(raw) + buckets - 1) / buckets
		for start := 0; start < len(raw); start += width {
			end := min(start+width, len(raw))
			selected[start] = true
			selected[end-1] = true
			for metric := 0; metric < 5; metric++ {
				lo, hi := -1, -1
				for i := start; i < end; i++ {
					v := values(raw[i])[metric]
					if v == nil {
						continue
					}
					if lo < 0 || *v < *values(raw[lo])[metric] {
						lo = i
					}
					if hi < 0 || *v > *values(raw[hi])[metric] {
						hi = i
					}
				}
				if lo >= 0 {
					selected[lo] = true
					selected[hi] = true
				}
			}
		}
	}
	out := make([]FabricDDMPoint, 0, min(len(raw), maxPoints))
	previous := -1
	for i, p := range raw {
		if !selected[i] {
			continue
		}
		p.ContinuousFromPrevious = previous >= 0 && breaks[i] == breaks[previous]
		out = append(out, p)
		previous = i
	}
	return out
}
