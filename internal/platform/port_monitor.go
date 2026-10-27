package platform

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

type PortMonitorItem struct {
	PhysicalPortID string               `json:"physical_port_id"`
	Plan           MappingPortPlan      `json:"plan"`
	Binding        *MappingBinding      `json:"binding"`
	MappingState   string               `json:"mapping_state"`
	State          string               `json:"state"`
	Reason         string               `json:"reason"`
	Observations   []MonitorObservation `json:"observations"`
	Alerts         []MonitorAlert       `json:"alerts"`
	OpenIssueIDs   []string             `json:"open_issue_ids"`
	EventCount     int64                `json:"event_count"`
}
type PortMonitorSummary struct {
	Total      int `json:"total"`
	Up         int `json:"up"`
	Down       int `json:"down"`
	Degraded   int `json:"degraded"`
	Flapping   int `json:"flapping"`
	Unknown    int `json:"unknown"`
	Disabled   int `json:"disabled"`
	Maintained int `json:"maintained"`
}
type PortMonitorPanelCell struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Row            int    `json:"row"`
	Column         int    `json:"column"`
	PhysicalPortID string `json:"physical_port_id"`
	Reason         string `json:"reason"`
}
type PortMonitorPanel struct {
	State            string                 `json:"state"`
	ModelVersionID   string                 `json:"model_version_id"`
	ModelVersion     int64                  `json:"model_version"`
	ModelContentHash string                 `json:"model_content_hash"`
	Rows             int                    `json:"rows"`
	Columns          int                    `json:"columns"`
	Cells            []PortMonitorPanelCell `json:"cells"`
}
type PortMonitorSnapshot struct {
	Panel             *PortMonitorPanel  `json:"panel"`
	MappingValidUntil *time.Time         `json:"mapping_valid_until"`
	ResyncRequired    bool               `json:"resync_required"`
	ProjectID         string             `json:"project_id"`
	AssetID           string             `json:"asset_id"`
	MachineID         string             `json:"machine_id"`
	AsOf              time.Time          `json:"as_of"`
	MappingRevisionID string             `json:"mapping_revision_id"`
	MappingState      string             `json:"mapping_state"`
	MappingReason     string             `json:"mapping_reason"`
	NetworkState      string             `json:"network_state"`
	Stage             string             `json:"stage"`
	Summary           PortMonitorSummary `json:"summary"`
	Items             []PortMonitorItem  `json:"items"`
}

// A projection of current mapping and already-ingested observations. There is
// no collector here and no OS-name fallback when a physical binding is unknown.
func (s *Store) PortMonitorSnapshot(ctx context.Context, a *AuthSession, project, asset string) (*PortMonitorSnapshot, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out, e := s.portMonitorSnapshotTx(ctx, tx, project, asset)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) portMonitorSnapshotTx(ctx context.Context, tx *sql.Tx, project, asset string) (*PortMonitorSnapshot, error) {
	var e error
	out := &PortMonitorSnapshot{ResyncRequired: true, ProjectID: project, AssetID: asset, AsOf: time.Now().UTC(), MappingState: "unbuilt", NetworkState: "unknown", Items: []PortMonitorItem{}}
	var state, projectState string
	if e = tx.QueryRowContext(ctx, `SELECT a.machine_id,a.state,p.state FROM project_assets a JOIN projects p ON p.id=a.project_id WHERE a.id=$1 AND a.project_id=$2`, asset, project).Scan(&out.MachineID, &state, &projectState); e != nil {
		return nil, e
	}
	out.Stage = monitorStage(state)
	stopped := state == "maintained" || projectState == "archived" || projectState == "delivered"
	revision, e := scanMappingRevision(tx.QueryRowContext(ctx, `SELECT `+mappingRevisionFields+` FROM mapping_revisions r JOIN mapping_assessments a ON a.id=r.assessment_id WHERE r.project_id=$1 AND r.asset_id=$2 ORDER BY r.version DESC LIMIT 1`, project, asset))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return nil, e
	}
	bindings := map[string]MappingBinding{}
	excluded := map[string]MappingExcludedPort{}
	if e == nil {
		if e = s.mappingValidityTx(ctx, tx, revision); e != nil {
			return nil, e
		}
		out.MappingValidUntil = revision.Candidate.ValidUntil
		out.MappingRevisionID = revision.ID
		out.MappingState = "verified"
		out.MappingReason = revision.InvalidationReason
		if revision.EffectiveVerdict != "PASS" {
			out.MappingState = "blocked"
		}
		for _, b := range revision.Candidate.Bindings {
			bindings[b.Plan.PhysicalPortID] = b
		}
		for _, p := range revision.Candidate.ExcludedPorts {
			excluded[p.Plan.PhysicalPortID] = p
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,port_name,coalesce(port_index,''),coalesce(rail_label,''),coalesce(parent_uid,'') FROM physical_ports WHERE machine_id=$1 ORDER BY port_index,id LIMIT 2049`, out.MachineID)
	if e != nil {
		return nil, e
	}
	indices := map[string]int{}
	for rows.Next() {
		var item PortMonitorItem
		item.Observations = []MonitorObservation{}
		item.Alerts = []MonitorAlert{}
		item.OpenIssueIDs = []string{}
		item.MappingState = "pending"
		if e = rows.Scan(&item.PhysicalPortID, &item.Plan.Name, &item.Plan.PortIndex, &item.Plan.Rail, &item.Plan.ComponentID); e != nil {
			rows.Close()
			return nil, e
		}
		item.Plan.PhysicalPortID = item.PhysicalPortID
		if b, ok := bindings[item.PhysicalPortID]; ok {
			item.Plan = b.Plan
			item.Binding = &b
			item.MappingState = out.MappingState
		}
		if p, ok := excluded[item.PhysicalPortID]; ok {
			item.Plan = p.Plan
			item.MappingState = out.MappingState
		}
		indices[item.PhysicalPortID] = len(out.Items)
		out.Items = append(out.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(out.Items) > 2048 {
		return nil, apiError(409, "MONITOR_SCOPE_LIMIT", "Physical port scope exceeds snapshot limit")
	}
	out.Panel, e = portMonitorPanelTx(ctx, tx, project, asset, out.Items)
	if e != nil {
		return nil, e
	}
	// Source and method remain separate. Simulated data never supplies live lamps.
	observed, e := tx.QueryContext(ctx, `SELECT `+monitorObservationFields+` FROM monitor_metric_latest latest JOIN monitor_metric_samples sample ON sample.sequence=latest.sample_sequence WHERE latest.project_id=$1 AND latest.machine_id=$2 AND sample.origin='real' AND sample.physical_port_id IS NOT NULL AND sample.mapping_revision_id=$3 ORDER BY sample.source,sample.method,sample.metric_key,sample.entity_key`, project, out.MachineID, out.MappingRevisionID)
	if e != nil {
		return nil, e
	}
	for observed.Next() {
		observation, e := scanMonitorObservation(observed, out.AsOf)
		if e != nil {
			observed.Close()
			return nil, e
		}
		if index, ok := indices[observation.PhysicalPortID]; ok {
			out.Items[index].Observations = append(out.Items[index].Observations, *observation)
		}
	}
	e = observed.Err()
	observed.Close()
	if e != nil {
		return nil, e
	}
	alerts, e := tx.QueryContext(ctx, `SELECT `+monitorAlertFields+` FROM monitor_alerts WHERE project_id=$1 AND machine_id=$2 AND origin='real' AND physical_port_id IS NOT NULL ORDER BY last_at DESC,id DESC`, project, out.MachineID)
	if e != nil {
		return nil, e
	}
	for alerts.Next() {
		alert, e := scanMonitorAlert(alerts)
		if e != nil {
			alerts.Close()
			return nil, e
		}
		if index, ok := indices[alert.PhysicalPortID]; ok {
			out.Items[index].Alerts = append(out.Items[index].Alerts, *alert)
		}
	}
	e = alerts.Err()
	alerts.Close()
	if e != nil {
		return nil, e
	}
	// Fetch stable unresolved anchors and event counts once for this machine.
	incidentRows, e := tx.QueryContext(ctx, `SELECT issue.id,anchor->>'id' FROM issues issue CROSS JOIN LATERAL jsonb_array_elements(issue.anchors) anchor WHERE issue.project_id=$1 AND issue.state<>'closed' AND issue.source IN('g2','portmon','fabric') AND anchor->>'kind'='port' AND EXISTS(SELECT 1 FROM physical_ports port WHERE port.id=anchor->>'id' AND port.machine_id=$2) ORDER BY issue.id`, project, out.MachineID)
	if e != nil {
		return nil, e
	}
	for incidentRows.Next() {
		var id, port string
		if e = incidentRows.Scan(&id, &port); e != nil {
			incidentRows.Close()
			return nil, e
		}
		if index, ok := indices[port]; ok {
			out.Items[index].OpenIssueIDs = append(out.Items[index].OpenIssueIDs, id)
		}
	}
	e = incidentRows.Err()
	incidentRows.Close()
	if e != nil {
		return nil, e
	}
	eventRows, e := tx.QueryContext(ctx, `SELECT alert.physical_port_id,count(*) FROM monitor_alert_events event JOIN monitor_alerts alert ON alert.id=event.alert_id WHERE alert.project_id=$1 AND alert.machine_id=$2 AND alert.physical_port_id IS NOT NULL AND alert.origin='real' GROUP BY alert.physical_port_id`, project, out.MachineID)
	if e != nil {
		return nil, e
	}
	for eventRows.Next() {
		var port string
		var count int64
		if e = eventRows.Scan(&port, &count); e != nil {
			eventRows.Close()
			return nil, e
		}
		if index, ok := indices[port]; ok {
			out.Items[index].EventCount = count
		}
	}
	e = eventRows.Err()
	eventRows.Close()
	if e != nil {
		return nil, e
	}
	for i := range out.Items {
		item := &out.Items[i]
		item.State, item.Reason = portMonitorState(*item, out.MappingRevisionID, stopped)
		switch item.State {
		case "up":
			out.Summary.Up++
		case "down":
			out.Summary.Down++
		case "degraded":
			out.Summary.Degraded++
		case "flapping":
			out.Summary.Flapping++
		case "disabled":
			out.Summary.Disabled++
		case "maintained":
			out.Summary.Maintained++
		default:
			out.Summary.Unknown++
		}
	}
	out.Summary.Total = len(out.Items)
	out.NetworkState = portMonitorNetworkState(out.Summary, out.MappingState, stopped)
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i].Plan, out.Items[j].Plan
		if a.ComponentID != b.ComponentID {
			return a.ComponentID < b.ComponentID
		}
		if a.PortIndex != b.PortIndex {
			return a.PortIndex < b.PortIndex
		}
		return a.PhysicalPortID < b.PhysicalPortID
	})
	return out, nil
}

func portMonitorState(item PortMonitorItem, revision string, stopped bool) (string, string) {
	if stopped {
		return "maintained", "inband_stopped"
	}
	if item.MappingState != "verified" || revision == "" {
		return "unknown", "mapping_pending_or_invalid"
	}
	if item.Plan.Role == "unused" {
		return "disabled", "role_not_enabled"
	}
	if item.Binding == nil {
		return "unknown", "physical_binding_missing"
	}
	freshAlert := func(alert MonitorAlert) bool {
		if alert.SampleSequence == nil {
			return false
		}
		for _, observation := range item.Observations {
			if observation.Sequence == *alert.SampleSequence && observation.Origin == "real" && observation.Freshness == "fresh" {
				return true
			}
		}
		return false
	}
	for _, alert := range item.Alerts {
		if !freshAlert(alert) || alert.MappingRevisionID != revision || alert.State != "firing" && alert.State != "suppressed" {
			continue
		}
		if alert.RuleID == "link-flapping" {
			return "flapping", "link_flapping"
		}
	}
	// A fresh down is affirmative evidence, even if another source is missing.
	var hostLink *MonitorObservation
	degraded := false
	for i := range item.Observations {
		sample := &item.Observations[i]
		if sample.Origin != "real" || sample.MappingRevisionID != revision || sample.Freshness != "fresh" || sample.Sample.Quality != "good" {
			continue
		}
		if sample.Sample.Key == "link.state" {
			if sample.Sample.State == "down" {
				return "down", "observed_link_down"
			}
			if sample.Source == "host" && (hostLink == nil || sample.Sample.ObservedAt.After(hostLink.Sample.ObservedAt)) {
				hostLink = sample
			}
		}
	}
	for _, alert := range item.Alerts {
		if freshAlert(alert) && alert.MappingRevisionID == revision && (alert.State == "firing" || alert.State == "suppressed") {
			degraded = true
		}
	}
	if len(item.OpenIssueIDs) > 0 {
		return "degraded", "unresolved_network_issue"
	}
	if degraded {
		return "degraded", "active_port_alert"
	}
	if hostLink == nil || hostLink.Sample.State != "up" {
		return "unknown", "fresh_host_link_missing"
	}
	if hostLink.Method == "ssh" {
		return "degraded", "agentless_snapshot"
	}
	return "up", "fresh_enabled_link"
}
func portMonitorNetworkState(s PortMonitorSummary, mapping string, stopped bool) string {
	if stopped {
		return "maintained"
	}
	if s.Down+s.Degraded+s.Flapping > 0 {
		return "abnormal"
	}
	if mapping != "verified" || s.Unknown > 0 || s.Up == 0 {
		return "unknown"
	}
	return "normal"
}
