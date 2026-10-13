package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// WP-3.3 nettest-svc (FR-11): the RoCE connectivity test module that sits
// between line-order confirmation (G2/G3) and the NCCL suite (G6), and feeds
// the G5 gate. The five layers share one per-NIC-pair result model: L1 is a
// per-port self baseline read from the port panel (never re-collected),
// L2-L4 are per-NIC-pair mesh measurements, L5 is per-rail platform
// counters. Measurements arrive through the mock ingest channel (ADR-14);
// the engine owns the plan, the T1-T6 classification, the coverage
// accounting, the G5 evidence and the FR-4.7 ticket loop.

// NettestPlatform is the cross-system handshake with the network platform:
// a run refuses to start unless the handshake was refreshed and reports
// ready, because the switch-side PFC/ECN counters come from there.
type NettestPlatform struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	Status      string         `json:"status"`
	Detail      map[string]any `json:"detail"`
	RefreshedBy string         `json:"refreshed_by"`
	RefreshedAt time.Time      `json:"refreshed_at"`
}

type NettestPlatformInput struct {
	Status string         `json:"status"`
	Detail map[string]any `json:"detail"`
}

type NettestPrecheck struct {
	Check  string `json:"check"`
	State  string `json:"state"` // pass / fail / waived
	Detail string `json:"detail"`
}

type NettestLayerPlan struct {
	Layer string `json:"layer"`
	Kind  string `json:"kind"` // port_baseline / mesh / rail
	Pairs int    `json:"pairs"`
}

type NettestRun struct {
	ID         string             `json:"id"`
	ProjectID  string             `json:"project_id"`
	Profile    string             `json:"profile"`
	State      string             `json:"state"`
	Layers     []NettestLayerPlan `json:"layers"`
	Targets    []string           `json:"targets"`
	TargetHash string             `json:"target_hash"`
	BatchSize  int                `json:"batch_size"`
	Params     map[string]any     `json:"params"`
	Precheck   []NettestPrecheck  `json:"precheck"`
	Coverage   map[string]any     `json:"coverage"`
	Conclusion string             `json:"conclusion"`
	IssueID    string             `json:"issue_id,omitempty"`
	CreatedBy  string             `json:"created_by"`
	CreatedAt  time.Time          `json:"created_at"`
	StartedAt  *time.Time         `json:"started_at,omitempty"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
}

type NettestPlanInput struct {
	MachineIDs []string       `json:"machine_ids"`
	Profile    string         `json:"profile"`
	Params     map[string]any `json:"params"`
	IssueID    string         `json:"issue_id"` // FR-4.7 targeted re-verification
}

type NettestPair struct {
	ID             string         `json:"id"`
	RunID          string         `json:"run_id"`
	Layer          string         `json:"layer"`
	MachineID      string         `json:"machine_id"`
	Hostname       string         `json:"hostname,omitempty"`
	PortID         string         `json:"port_id"`
	PortName       string         `json:"port_name,omitempty"`
	PeerMachineID  string         `json:"peer_machine_id"`
	PeerHostname   string         `json:"peer_hostname,omitempty"`
	PeerPortID     string         `json:"peer_port_id,omitempty"`
	PeerPortName   string         `json:"peer_port_name,omitempty"`
	State          string         `json:"state"`
	BandwidthGbps  float64        `json:"bandwidth_gbps"`
	LossPPM        float64        `json:"loss_ppm"`
	PFCPause       int64          `json:"pfc_pause"`
	Hotspot        bool           `json:"hotspot"`
	Classification string         `json:"classification,omitempty"`
	Detail         map[string]any `json:"detail"`
	MeasuredAt     *time.Time     `json:"measured_at,omitempty"`
}

type NettestResultInput struct {
	Layer         string         `json:"layer"`
	MachineID     string         `json:"machine_id"`
	PortID        string         `json:"port_id"`
	PeerMachineID string         `json:"peer_machine_id"`
	PeerPortID    string         `json:"peer_port_id"`
	State         string         `json:"state"` // pass / fail
	BandwidthGbps float64        `json:"bandwidth_gbps"`
	LossPPM       float64        `json:"loss_ppm"`
	PFCPause      int64          `json:"pfc_pause"`
	Hotspot       bool           `json:"hotspot"`
	Failure       string         `json:"failure"` // raw observation, classified by the engine
	Detail        map[string]any `json:"detail"`
}

type NettestIngestInput struct {
	Results []NettestResultInput `json:"results"`
}

type NettestMatrixCell struct {
	MachineID     string  `json:"machine_id"`
	Hostname      string  `json:"hostname"`
	PeerMachineID string  `json:"peer_machine_id"`
	PeerHostname  string  `json:"peer_hostname"`
	Layer         string  `json:"layer"`
	Pairs         int     `json:"pairs"`
	Passed        int     `json:"passed"`
	Failed        int     `json:"failed"`
	BandwidthGbps float64 `json:"bandwidth_gbps"`
	State         string  `json:"state"` // pass / fail / pending
}

type NettestAdmission struct {
	MachineID   string `json:"machine_id"`
	Hostname    string `json:"hostname"`
	NetVerified bool   `json:"net_verified"`
	Waived      bool   `json:"waived"`
	Reason      string `json:"reason"`
}

// nettestParams is the FR-11.1/11.2 parameter surface. Keys are the PRD's
// nettest.* names; a run carries the frozen snapshot it planned with.
var nettestParams = map[string]bool{
	"nettest.l2_pair_sample":         true,
	"nettest.l2_batch_size":          true,
	"nettest.l3_pair_mode":           true,
	"nettest.l4_batch_size":          true,
	"nettest.l4_duration_seconds":    true,
	"nettest.required_path_coverage": true,
}

var nettestFailureClasses = map[string]string{
	"link_down":          "T1",
	"peer_mismatch":      "T2",
	"dom_degraded":       "T3",
	"flapping":           "T4",
	"crc_errors":         "T4",
	"nic_all_ports_down": "T5",
	"ip_conflict":        "T6",
	"vlan_unreachable":   "T6",
	"pfc_misconfig":      "T6",
	"mtu_mismatch":       "T6",
}

// classifyNettestFailure maps a raw observation to its T category. An
// unknown observation stays unclassified: the engine never guesses a
// category to make a ticket look complete.
func classifyNettestFailure(failure string) string {
	if category, ok := nettestFailureClasses[failure]; ok {
		return category
	}
	return ""
}

func nettestParamDefaults(profile string) map[string]any {
	params := map[string]any{}
	if profile == "full" {
		params["nettest.l3_pair_mode"] = "full"
		params["nettest.l4_duration_seconds"] = 300
		params["nettest.required_path_coverage"] = 1.0
	} else {
		params["nettest.l3_pair_mode"] = "matrix-sample"
		params["nettest.l4_duration_seconds"] = 30
		params["nettest.required_path_coverage"] = 0.25
	}
	params["nettest.l2_pair_sample"] = 32
	params["nettest.l2_batch_size"] = 8
	params["nettest.l4_batch_size"] = 8
	return params
}

// RefreshNettestPlatform records the cross-system handshake (mock channel,
// ADR-14): the operator or the platform adapter reports readiness before a
// run is allowed to start.
func (s *Store) RefreshNettestPlatform(ctx context.Context, a *AuthSession, project *ProjectSummary, input NettestPlatformInput, trace string) (*NettestPlatform, error) {
	switch input.Status {
	case "ready", "degraded", "down", "unknown":
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "platform status must be ready, degraded, down or unknown")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	detailJSON, _ := json.Marshal(input.Detail)
	now := time.Now().UTC()
	platform := &NettestPlatform{ID: RandomID(), ProjectID: project.ID, Status: input.Status,
		Detail: input.Detail, RefreshedBy: a.User.ID, RefreshedAt: now}
	if platform.Detail == nil {
		platform.Detail = map[string]any{}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO nettest_platform(id,project_id,status,detail,refreshed_by,refreshed_at)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT(project_id) DO UPDATE SET status=EXCLUDED.status,detail=EXCLUDED.detail,refreshed_by=EXCLUDED.refreshed_by,refreshed_at=EXCLUDED.refreshed_at`,
		platform.ID, project.ID, input.Status, string(detailJSON), a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "nettest.platform_refresh", platform.ID, trace, map[string]any{"status": input.Status}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return platform, nil
}

// GetNettestPlatform returns the project's handshake state, or nil when the
// platform was never refreshed (a run then refuses to start).
func (s *Store) GetNettestPlatform(ctx context.Context, a *AuthSession, project *ProjectSummary) (*NettestPlatform, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var (
		state       NettestPlatform
		detailJSON  []byte
		refreshedAt time.Time
	)
	if e = tx.QueryRowContext(ctx, `SELECT id,project_id,status,detail,refreshed_by,refreshed_at FROM nettest_platform WHERE project_id=$1`,
		project.ID).Scan(&state.ID, &state.ProjectID, &state.Status, &detailJSON, &state.RefreshedBy, &refreshedAt); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, e
	}
	state.Detail = map[string]any{}
	if len(detailJSON) > 0 {
		_ = json.Unmarshal(detailJSON, &state.Detail)
	}
	state.RefreshedAt = refreshedAt
	return &state, nil
}

// PlanNettestRun runs the FR-11.2 precheck and, only when every condition
// holds, materialises the five-layer plan. A refused start returns the
// verdicts instead of a run — the caller sees exactly which condition
// blocked it.
func (s *Store) PlanNettestRun(ctx context.Context, a *AuthSession, project *ProjectSummary, input NettestPlanInput, trace string) (*NettestRun, error) {
	if len(input.MachineIDs) == 0 || len(input.MachineIDs) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "machine_ids must contain 1-500 entries")
	}
	if input.Profile != "quick" && input.Profile != "full" {
		return nil, apiError(400, "VALIDATION_ERROR", "profile must be quick or full")
	}
	params := nettestParamDefaults(input.Profile)
	for key, value := range input.Params {
		if !nettestParams[key] {
			return nil, apiError(400, "VALIDATION_ERROR", fmt.Sprintf("unknown nettest parameter %q", key))
		}
		params[key] = value
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	// An issue-bound re-verification exempts the fault itself per FR-4.7 but
	// still checks project permissions, resource exclusion and the rest.
	issueBound := false
	if input.IssueID != "" {
		issue, e := loadIssue(ctx, tx, project.ID, input.IssueID)
		if e != nil {
			return nil, e
		}
		if issue.MachineID == "" {
			return nil, apiError(409, "VALIDATION_ERROR", "issue carries no machine; cannot scope the re-verification")
		}
		input.MachineIDs = []string{issue.MachineID}
		issueBound = true
	}
	precheck, e := s.nettestPrecheck(ctx, tx, project.ID, input.MachineIDs, issueBound)
	if e != nil {
		return nil, e
	}
	blocked := false
	for _, check := range precheck {
		if check.State == "fail" {
			blocked = true
		}
	}
	if blocked {
		return nil, apiError(409, "PRECHECK_FAILED", "the run precheck refused the start: "+nettestPrecheckSummary(precheck))
	}
	layers, e := s.nettestLayerPlan(ctx, tx, project.ID, input.MachineIDs)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	run := &NettestRun{
		ID: RandomID(), ProjectID: project.ID, Profile: input.Profile, State: "planned",
		Layers: layers, Targets: append([]string(nil), input.MachineIDs...), TargetHash: targetHash(input.MachineIDs),
		BatchSize: intFromAny(params["nettest.l2_batch_size"]), Params: params, Precheck: precheck,
		Coverage: map[string]any{}, IssueID: input.IssueID, CreatedBy: a.User.ID, CreatedAt: now,
	}
	layersJSON, _ := json.Marshal(layers)
	targetsJSON, _ := json.Marshal(run.Targets)
	precheckJSON, _ := json.Marshal(precheck)
	paramsJSON, _ := json.Marshal(params)
	if _, e = tx.ExecContext(ctx, `INSERT INTO nettest_runs(id,project_id,profile,state,layers,targets,target_hash,batch_size,params,precheck,coverage,conclusion,issue_id,created_by,created_at)
		VALUES($1,$2,$3,'planned',$4,$5,$6,$7,$8,$9,'{}','',$10,$11,$12)`,
		run.ID, project.ID, input.Profile, string(layersJSON), string(targetsJSON), run.TargetHash, run.BatchSize,
		string(paramsJSON), string(precheckJSON), input.IssueID, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "nettest.plan", run.ID, trace, map[string]any{
		"targets": len(run.Targets), "profile": input.Profile, "issue": input.IssueID,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return run, nil
}

// nettestPrecheck evaluates the FR-11.2 conditions: valid G4 evidence for
// every target, the frozen mapping with roles in place, no unhandled
// blocking alert (waived for an issue-bound re-verification) and a fresh,
// ready network-platform handshake.
func (s *Store) nettestPrecheck(ctx context.Context, tx *sql.Tx, projectID string, machineIDs []string, issueBound bool) ([]NettestPrecheck, error) {
	out := []NettestPrecheck{}
	g4Fail := []string{}
	mappingFail := []string{}
	for _, machineID := range machineIDs {
		required, verified, e := machineConfigState(ctx, tx, projectID, machineID)
		if e != nil {
			return nil, e
		}
		if required == 0 || verified < required {
			g4Fail = append(g4Fail, fmt.Sprintf("%s(%d/%d verified)", machineID, verified, required))
		}
		var revision bool
		var unmapped int
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM mapping_revisions WHERE project_id=$1 AND machine_id=$2)", projectID, machineID).Scan(&revision); e != nil {
			return nil, e
		}
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM physical_ports p
			WHERE p.machine_id=$1 AND p.role<>'unused'
			  AND NOT EXISTS (SELECT 1 FROM mapping_entries e WHERE e.port_id = p.id
			    AND e.revision_id = (SELECT r.id FROM mapping_revisions r WHERE r.project_id=$2 AND r.machine_id=$1 ORDER BY r.sequence DESC LIMIT 1))`,
			machineID, projectID).Scan(&unmapped); e != nil {
			return nil, e
		}
		if !revision || unmapped > 0 {
			mappingFail = append(mappingFail, fmt.Sprintf("%s(revision=%t unmapped=%d)", machineID, revision, unmapped))
		}
	}
	if len(g4Fail) == 0 {
		out = append(out, NettestPrecheck{Check: "g4", State: "pass", Detail: "every target's required renders carry a verified read-back"})
	} else {
		out = append(out, NettestPrecheck{Check: "g4", State: "fail", Detail: "targets without complete verified G4 evidence: " + strings.Join(g4Fail, ",")})
	}
	if len(mappingFail) == 0 {
		out = append(out, NettestPrecheck{Check: "mapping", State: "pass", Detail: "frozen mapping revision published with every active port mapped"})
	} else {
		out = append(out, NettestPrecheck{Check: "mapping", State: "fail", Detail: "targets without a frozen mapping or with unmapped active ports: " + strings.Join(mappingFail, ",")})
	}
	openAlerts := 0
	for _, machineID := range machineIDs {
		var count int
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mon_alerts WHERE project_id=$1 AND machine_id=$2 AND closed_at IS NULL", projectID, machineID).Scan(&count); e != nil {
			return nil, e
		}
		openAlerts += count
	}
	switch {
	case openAlerts == 0:
		out = append(out, NettestPrecheck{Check: "alerts", State: "pass", Detail: "no unhandled alert on the targets"})
	case issueBound:
		out = append(out, NettestPrecheck{Check: "alerts", State: "waived", Detail: fmt.Sprintf("%d open alert(s) exempted: issue-bound diagnostic re-verification (FR-4.7)", openAlerts)})
	default:
		out = append(out, NettestPrecheck{Check: "alerts", State: "fail", Detail: fmt.Sprintf("%d unhandled alert(s) would block a plain run", openAlerts)})
	}
	var platformStatus string
	var refreshedAt sql.NullTime
	e := tx.QueryRowContext(ctx, "SELECT status, refreshed_at FROM nettest_platform WHERE project_id=$1", projectID).Scan(&platformStatus, &refreshedAt)
	switch {
	case errors.Is(e, sql.ErrNoRows):
		out = append(out, NettestPrecheck{Check: "platform", State: "fail", Detail: "network platform handshake never refreshed"})
	case e != nil:
		return nil, e
	case platformStatus != "ready":
		out = append(out, NettestPrecheck{Check: "platform", State: "fail", Detail: "network platform reports " + platformStatus})
	case !refreshedAt.Valid || time.Since(refreshedAt.Time) > time.Hour:
		out = append(out, NettestPrecheck{Check: "platform", State: "fail", Detail: "network platform handshake is stale; refresh before the run"})
	default:
		out = append(out, NettestPrecheck{Check: "platform", State: "pass", Detail: "network platform ready, handshake fresh"})
	}
	return out, nil
}

func nettestPrecheckSummary(checks []NettestPrecheck) string {
	parts := []string{}
	for _, check := range checks {
		if check.State == "fail" {
			parts = append(parts, check.Check+": "+check.Detail)
		}
	}
	return strings.Join(parts, "; ")
}

// nettestLayerPlan derives the five-layer plan from the frozen mapping:
// L1 baselines every active port, L2-L4 mesh the same-rail ports across the
// targets, L5 covers each machine×rail with the platform counters.
func (s *Store) nettestLayerPlan(ctx context.Context, tx *sql.Tx, projectID string, machineIDs []string) ([]NettestLayerPlan, error) {
	type port struct {
		id      string
		machine string
		rail    int
		iface   string
	}
	ports := []port{}
	rails := map[string]map[int]bool{}
	for _, machineID := range machineIDs {
		rows, e := tx.QueryContext(ctx, `SELECT p.id, p.rail, e.os_interface FROM physical_ports p
			JOIN mapping_entries e ON e.port_id = p.id
			WHERE p.machine_id=$1 AND p.role<>'unused'
			  AND e.revision_id = (SELECT r.id FROM mapping_revisions r WHERE r.project_id=$2 AND r.machine_id=$1 ORDER BY r.sequence DESC LIMIT 1)
			ORDER BY p.port_name`, machineID, projectID)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var p port
			p.machine = machineID
			if e = rows.Scan(&p.id, &p.rail, &p.iface); e != nil {
				rows.Close()
				return nil, e
			}
			ports = append(ports, p)
			if rails[machineID] == nil {
				rails[machineID] = map[int]bool{}
			}
			rails[machineID][p.rail] = true
		}
		if e = rows.Err(); e != nil {
			rows.Close()
			return nil, e
		}
		rows.Close()
	}
	// Cross-machine same-rail unordered port pairs (the L2-L4 mesh).
	mesh := 0
	for i := 0; i < len(ports); i++ {
		for j := i + 1; j < len(ports); j++ {
			if ports[i].machine != ports[j].machine && ports[i].rail == ports[j].rail {
				mesh++
			}
		}
	}
	railCount := 0
	for _, byRail := range rails {
		railCount += len(byRail)
	}
	return []NettestLayerPlan{
		{Layer: "L1", Kind: "port_baseline", Pairs: len(ports)},
		{Layer: "L2", Kind: "mesh", Pairs: mesh},
		{Layer: "L3", Kind: "mesh", Pairs: mesh},
		{Layer: "L4", Kind: "mesh", Pairs: mesh},
		{Layer: "L5", Kind: "rail", Pairs: railCount},
	}, nil
}

// IngestNettestResults is the mock measurement channel (ADR-14): the
// per-NIC-pair results arrive here, the engine classifies every failure and
// stores the pair. A pair is identified by its natural key, so a repeated
// measurement updates the row instead of duplicating it.
func (s *Store) IngestNettestResults(ctx context.Context, a *AuthSession, project *ProjectSummary, runID string, input NettestIngestInput, trace string) (*NettestRun, error) {
	if len(input.Results) == 0 || len(input.Results) > 2000 {
		return nil, apiError(400, "VALIDATION_ERROR", "results must contain 1-2000 entries")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	run, e := loadNettestRun(ctx, tx, project.ID, runID)
	if e != nil {
		return nil, e
	}
	if run.State != "planned" && run.State != "running" && run.State != "finished" {
		return nil, apiError(409, "NETTEST_STATE", "results ingest into a planned, running or finished run only")
	}
	now := time.Now().UTC()
	for _, result := range input.Results {
		switch result.State {
		case "pass", "fail":
		default:
			return nil, apiError(400, "VALIDATION_ERROR", "pair state must be pass or fail")
		}
		if result.Layer != "L1" && result.Layer != "L2" && result.Layer != "L3" && result.Layer != "L4" && result.Layer != "L5" {
			return nil, apiError(400, "VALIDATION_ERROR", "layer must be L1-L5")
		}
		classification := ""
		if result.State == "fail" {
			classification = classifyNettestFailure(result.Failure)
		}
		detailJSON, _ := json.Marshal(result.Detail)
		if detailJSON == nil {
			detailJSON = []byte("{}")
		}
		var existing string
		e = tx.QueryRowContext(ctx, `SELECT id FROM nettest_pairs
			WHERE run_id=$1 AND layer=$2 AND machine_id=$3 AND port_id=$4 AND peer_machine_id=$5 AND peer_port_id IS NOT DISTINCT FROM $6`,
			run.ID, result.Layer, result.MachineID, result.PortID, result.PeerMachineID, nullString(result.PeerPortID)).Scan(&existing)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if existing != "" {
			if _, e = tx.ExecContext(ctx, `UPDATE nettest_pairs SET state=$2,bandwidth_gbps=$3,loss_ppm=$4,pfc_pause=$5,hotspot=$6,classification=$7,detail=$8,measured_at=$9 WHERE id=$1`,
				existing, result.State, result.BandwidthGbps, result.LossPPM, result.PFCPause, result.Hotspot, classification, string(detailJSON), now); e != nil {
				return nil, e
			}
			continue
		}
		if run.State == "finished" {
			// A finished run only accepts re-measurements of pairs it already
			// carries (the FR-4.7 targeted re-verification); it never grows
			// new pairs after the conclusion was recorded.
			return nil, apiError(409, "NETTEST_STATE", "a finished run only re-measures pairs it already carries; plan a new run for new pairs")
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO nettest_pairs(id,run_id,project_id,layer,machine_id,port_id,peer_machine_id,peer_port_id,state,bandwidth_gbps,loss_ppm,pfc_pause,hotspot,classification,detail,measured_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
			RandomID(), run.ID, project.ID, result.Layer, result.MachineID, result.PortID, result.PeerMachineID,
			nullString(result.PeerPortID), result.State, result.BandwidthGbps, result.LossPPM, result.PFCPause,
			result.Hotspot, classification, string(detailJSON), now); e != nil {
			return nil, e
		}
	}
	if run.State == "planned" {
		if _, e = tx.ExecContext(ctx, "UPDATE nettest_runs SET state='running', started_at=$2 WHERE id=$1", run.ID, now); e != nil {
			return nil, e
		}
		run.State, run.StartedAt = "running", &now
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "nettest.ingest", run.ID, trace, map[string]any{
		"results": len(input.Results),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return run, nil
}

// FinishNettestRun closes the run: it accounts coverage per layer, records
// the G5 evidence (PASS only when every planned pair measured and passed)
// and opens the FR-4.7 tickets for the classified failures.
func (s *Store) FinishNettestRun(ctx context.Context, a *AuthSession, project *ProjectSummary, runID string, trace string) (*NettestRun, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	run, e := loadNettestRun(ctx, tx, project.ID, runID)
	if e != nil {
		return nil, e
	}
	if run.State != "running" && run.State != "planned" && run.State != "finished" {
		return nil, apiError(409, "NETTEST_STATE", "only a planned, running or finished run finishes")
	}
	now := time.Now().UTC()
	coverage := map[string]any{}
	complete, sampled := true, false
	failedTotal, unclassified := 0, []string{}
	required := 1.0
	if value, ok := run.Params["nettest.required_path_coverage"].(float64); ok && value > 0 {
		required = value
	}
	for _, layer := range run.Layers {
		var measured, passed, failed int
		if e = tx.QueryRowContext(ctx, `SELECT
				COUNT(*) FILTER (WHERE state<>'pending'),
				COUNT(*) FILTER (WHERE state='pass'),
				COUNT(*) FILTER (WHERE state='fail')
			FROM nettest_pairs WHERE run_id=$1 AND layer=$2`, run.ID, layer.Layer).Scan(&measured, &passed, &failed); e != nil {
			return nil, e
		}
		if measured < int(math.Ceil(float64(layer.Pairs)*required)) {
			complete = false
		}
		if measured < layer.Pairs {
			sampled = true
		}
		failedTotal += failed
		ratio := 0.0
		if layer.Pairs > 0 {
			ratio = float64(measured) / float64(layer.Pairs)
		}
		coverage[layer.Layer] = map[string]any{
			"kind": layer.Kind, "planned": layer.Pairs, "measured": measured,
			"passed": passed, "failed": failed, "coverage": ratio,
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM nettest_pairs WHERE run_id=$1 AND state='fail' AND classification=''`, run.ID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var pairID string
		if e = rows.Scan(&pairID); e != nil {
			rows.Close()
			return nil, e
		}
		unclassified = append(unclassified, pairID)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return nil, e
	}
	rows.Close()
	conclusion := ""
	switch {
	case !complete:
		// Coverage short of the plan: the run cannot conclude, and no G5
		// evidence is written — an unfinished run is not a pass.
	case failedTotal > 0:
		conclusion = "FAIL"
	default:
		conclusion = "PASS"
	}
	coverage["complete"] = complete
	coverage["sampled"] = sampled
	coverage["unclassified_failures"] = unclassified
	run.Coverage, run.Conclusion, run.FinishedAt = coverage, conclusion, &now
	run.State = "finished"
	// G5 evidence: a sampled tier never claims full-path coverage.
	if conclusion != "" {
		sampled, _ := coverage["sampled"].(bool)
		detail, _ := json.Marshal(map[string]any{
			"run_id": run.ID, "profile": run.Profile, "layers": coverage, "sampled": sampled,
		})
		evidenceID := RandomID()
		if _, e = tx.ExecContext(ctx, `INSERT INTO gate_evidences(id,project_id,gate,target_hash,targets,conclusion,detail,decided_by,valid_until,created_at)
			VALUES($1,$2,'G5',$3,$4,$5,$6,$7,$8,$9)`,
			evidenceID, project.ID, run.TargetHash, run.Targets, conclusion, string(detail), a.User.ID, now.Add(gateTTL), now); e != nil {
			return nil, e
		}
		run.Coverage["g5_evidence"] = evidenceID
	}
	// T-class tickets for the classified failures (deduplicated per pair).
	if conclusion == "FAIL" {
		pairRows, e := tx.QueryContext(ctx, `SELECT id FROM nettest_pairs WHERE run_id=$1 AND state='fail' AND classification<>'' ORDER BY id`, run.ID)
		if e != nil {
			return nil, e
		}
		opened := []string{}
		failedPairs := []string{}
		for pairRows.Next() {
			var pairID string
			if e = pairRows.Scan(&pairID); e != nil {
				pairRows.Close()
				return nil, e
			}
			failedPairs = append(failedPairs, pairID)
		}
		if e = pairRows.Err(); e != nil {
			pairRows.Close()
			return nil, e
		}
		pairRows.Close()
		for _, pairID := range failedPairs {
			var exists bool
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issues WHERE project_id=$1 AND difference_id=$2 AND status IN('open','assigned','in_repair'))`,
				project.ID, pairID).Scan(&exists); e != nil {
				pairRows.Close()
				return nil, e
			}
			if exists {
				continue
			}
			issue, e := issueFromNettestPairTx(ctx, tx, project.ID, a.User.ID, pairID, "")
			if e != nil {
				return nil, e
			}
			opened = append(opened, issue.ID)
		}
		run.Coverage["tickets"] = opened
	}
	// One final write carries the complete coverage map — including the G5
	// evidence id and the opened tickets — so a reload sees the same run.
	coverageJSON, _ := json.Marshal(run.Coverage)
	if _, e = tx.ExecContext(ctx, "UPDATE nettest_runs SET state='finished', conclusion=$2, coverage=$3, finished_at=$4 WHERE id=$1",
		run.ID, conclusion, string(coverageJSON), now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "nettest.finish", run.ID, trace, map[string]any{
		"conclusion": conclusion, "complete": complete,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return run, nil
}

// GetNettestRun returns the run with its per-layer coverage and its pairs.
func (s *Store) GetNettestRun(ctx context.Context, a *AuthSession, project *ProjectSummary, runID string) (*NettestRun, []NettestPair, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, nil, e
	}
	run, e := loadNettestRun(ctx, tx, project.ID, runID)
	if e != nil {
		return nil, nil, e
	}
	pairs, e := loadNettestPairs(ctx, tx, run.ID)
	if e != nil {
		return nil, nil, e
	}
	return run, pairs, nil
}

// ListNettestRuns returns the project's runs, newest first.
func (s *Store) ListNettestRuns(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[NettestRun], error) {
	out := NewPage[NettestRun](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	var total int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM nettest_runs WHERE project_id=$1", project.ID).Scan(&total); e != nil {
		return out, e
	}
	out.Total = total
	rows, e := tx.QueryContext(ctx, `SELECT id,project_id,profile,state,layers,targets,target_hash,batch_size,params,precheck,coverage,conclusion,issue_id,created_by,created_at,started_at,finished_at
		FROM nettest_runs WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		run, e := scanNettestRun(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *run)
	}
	return out, rows.Err()
}

// GetNettestMatrix aggregates the per-pair results into the FR-11.4 rail
// heat matrix: one cell per (machine, peer machine, layer).
func (s *Store) GetNettestMatrix(ctx context.Context, a *AuthSession, project *ProjectSummary, runID, layer string) ([]NettestMatrixCell, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	run, e := loadNettestRun(ctx, tx, project.ID, runID)
	if e != nil {
		return nil, e
	}
	query := `SELECT p.machine_id, ma.hostname, p.peer_machine_id, mb.hostname, p.layer,
			COUNT(*), COUNT(*) FILTER (WHERE p.state='pass'), COUNT(*) FILTER (WHERE p.state='fail'),
			COALESCE(MAX(p.bandwidth_gbps),0)
		FROM nettest_pairs p
		JOIN project_assets ma ON ma.machine_id=p.machine_id AND ma.project_id=$2
		JOIN project_assets mb ON mb.machine_id=p.peer_machine_id AND mb.project_id=$2
		WHERE p.run_id=$1`
	args := []any{run.ID, project.ID}
	if layer != "" {
		query += " AND p.layer=$3"
		args = append(args, layer)
	}
	query += " GROUP BY p.machine_id, ma.hostname, p.peer_machine_id, mb.hostname, p.layer ORDER BY ma.hostname, mb.hostname, p.layer"
	rows, e := tx.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []NettestMatrixCell{}
	for rows.Next() {
		var cell NettestMatrixCell
		if e = rows.Scan(&cell.MachineID, &cell.Hostname, &cell.PeerMachineID, &cell.PeerHostname, &cell.Layer,
			&cell.Pairs, &cell.Passed, &cell.Failed, &cell.BandwidthGbps); e != nil {
			return nil, e
		}
		switch {
		case cell.Failed > 0:
			cell.State = "fail"
		case cell.Passed > 0:
			cell.State = "pass"
		default:
			cell.State = "pending"
		}
		out = append(out, cell)
	}
	return out, rows.Err()
}

// NettestAdmission reports the FR-11.6 NCCL admission state per machine: a
// target group may only start NCCL when its newest G5 evidence passes (or
// carries an unexpired waiver). A missing or failed gate is never a pass.
func (s *Store) NettestAdmission(ctx context.Context, a *AuthSession, project *ProjectSummary, machineIDs []string) ([]NettestAdmission, error) {
	if len(machineIDs) == 0 || len(machineIDs) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "machine_ids must contain 1-500 entries")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	rows, e := tx.QueryContext(ctx, `SELECT targets, conclusion, valid_until, created_at FROM gate_evidences
		WHERE project_id=$1 AND gate='G5' AND valid_until>$2 ORDER BY created_at DESC LIMIT 200`, project.ID, now)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	type verdict struct {
		conclusion string
		validUntil time.Time
	}
	newest := map[string]verdict{}
	for rows.Next() {
		var targetsJSON []byte
		var conclusion string
		var validUntil, createdAt time.Time
		if e = rows.Scan(&targetsJSON, &conclusion, &validUntil, &createdAt); e != nil {
			return nil, e
		}
		var targets []string
		if e = json.Unmarshal(targetsJSON, &targets); e != nil {
			return nil, e
		}
		for _, machineID := range targets {
			if _, ok := newest[machineID]; !ok {
				newest[machineID] = verdict{conclusion: conclusion, validUntil: validUntil}
			}
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	out := []NettestAdmission{}
	for _, machineID := range machineIDs {
		var hostname string
		if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, machineID).Scan(&hostname); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project: "+machineID)
			}
			return nil, e
		}
		admission := NettestAdmission{MachineID: machineID, Hostname: hostname, Reason: "no valid G5 evidence"}
		if entry, ok := newest[machineID]; ok {
			switch entry.conclusion {
			case "PASS":
				admission.NetVerified, admission.Waived, admission.Reason = true, false, "G5 evidence passes"
			case "WAIVED":
				admission.NetVerified, admission.Waived, admission.Reason = true, true, "G5 evidence waived until "+entry.validUntil.Format(time.RFC3339)
			default:
				admission.Reason = "newest G5 evidence is " + entry.conclusion
			}
		}
		out = append(out, admission)
	}
	return out, nil
}

func loadNettestRun(ctx context.Context, tx *sql.Tx, projectID, runID string) (*NettestRun, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,project_id,profile,state,layers,targets,target_hash,batch_size,params,precheck,coverage,conclusion,issue_id,created_by,created_at,started_at,finished_at
		FROM nettest_runs WHERE id=$1 AND project_id=$2`, runID, projectID)
	run, e := scanNettestRun(row)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "nettest run not found in this project")
		}
		return nil, e
	}
	return run, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNettestRun(row rowScanner) (*NettestRun, error) {
	var (
		run                                 NettestRun
		layersJSON, targetsJSON, paramsJSON []byte
		precheckJSON, coverageJSON          []byte
		conclusion, issueID                 sql.NullString
		startedAt, finishedAt               sql.NullTime
	)
	e := row.Scan(&run.ID, &run.ProjectID, &run.Profile, &run.State, &layersJSON, &targetsJSON, &run.TargetHash,
		&run.BatchSize, &paramsJSON, &precheckJSON, &coverageJSON, &conclusion, &issueID, &run.CreatedBy, &run.CreatedAt,
		&startedAt, &finishedAt)
	if e != nil {
		return nil, e
	}
	run.Conclusion, run.IssueID = conclusion.String, issueID.String
	if len(layersJSON) > 0 {
		_ = json.Unmarshal(layersJSON, &run.Layers)
	}
	if len(targetsJSON) > 0 {
		_ = json.Unmarshal(targetsJSON, &run.Targets)
	}
	if len(paramsJSON) > 0 {
		_ = json.Unmarshal(paramsJSON, &run.Params)
	}
	if len(precheckJSON) > 0 {
		_ = json.Unmarshal(precheckJSON, &run.Precheck)
	}
	if len(coverageJSON) > 0 {
		_ = json.Unmarshal(coverageJSON, &run.Coverage)
	}
	if startedAt.Valid {
		at := startedAt.Time
		run.StartedAt = &at
	}
	if finishedAt.Valid {
		at := finishedAt.Time
		run.FinishedAt = &at
	}
	if run.Params == nil {
		run.Params = map[string]any{}
	}
	if run.Coverage == nil {
		run.Coverage = map[string]any{}
	}
	if run.Precheck == nil {
		run.Precheck = []NettestPrecheck{}
	}
	if run.Layers == nil {
		run.Layers = []NettestLayerPlan{}
	}
	if run.Targets == nil {
		run.Targets = []string{}
	}
	return &run, nil
}

func loadNettestPairs(ctx context.Context, tx *sql.Tx, runID string) ([]NettestPair, error) {
	rows, e := tx.QueryContext(ctx, `SELECT p.id,p.run_id,p.layer,p.machine_id,ma.hostname,p.port_id,pp.port_name,
			p.peer_machine_id,mb.hostname,p.peer_port_id,COALESCE(qp.port_name,''),p.state,p.bandwidth_gbps,p.loss_ppm,
			p.pfc_pause,p.hotspot,p.classification,p.detail,p.measured_at
		FROM nettest_pairs p
		JOIN physical_ports pp ON pp.id=p.port_id
		JOIN project_assets ma ON ma.machine_id=p.machine_id
		JOIN project_assets mb ON mb.machine_id=p.peer_machine_id
		LEFT JOIN physical_ports qp ON qp.id=p.peer_port_id
		WHERE p.run_id=$1 ORDER BY p.layer, ma.hostname, mb.hostname, pp.port_name`, runID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []NettestPair{}
	for rows.Next() {
		var pair NettestPair
		var detailJSON []byte
		var measuredAt sql.NullTime
		var peerPortID, peerPortName, classification sql.NullString
		if e = rows.Scan(&pair.ID, &pair.RunID, &pair.Layer, &pair.MachineID, &pair.Hostname, &pair.PortID, &pair.PortName,
			&pair.PeerMachineID, &pair.PeerHostname, &peerPortID, &peerPortName, &pair.State, &pair.BandwidthGbps,
			&pair.LossPPM, &pair.PFCPause, &pair.Hotspot, &classification, &detailJSON, &measuredAt); e != nil {
			return nil, e
		}
		pair.PeerPortID, pair.PeerPortName, pair.Classification = peerPortID.String, peerPortName.String, classification.String
		if len(detailJSON) > 0 {
			_ = json.Unmarshal(detailJSON, &pair.Detail)
		}
		if pair.Detail == nil {
			pair.Detail = map[string]any{}
		}
		if measuredAt.Valid {
			at := measuredAt.Time
			pair.MeasuredAt = &at
		}
		out = append(out, pair)
	}
	return out, rows.Err()
}

// machineConfigState reports how many of the machine's required renderer
// kinds carry a verified newest apply — the G4 evidence the nettest
// precheck reads (shared with the dashboard G4 card).
func machineConfigState(ctx context.Context, tx *sql.Tx, projectID, machineID string) (required, verified int, err error) {
	kinds := g4RequiredKinds(ctx, tx, projectID, machineID)
	if len(kinds) == 0 {
		return 0, 0, nil
	}
	for _, kind := range kinds {
		var ok bool
		if e := tx.QueryRowContext(ctx, `SELECT verified FROM config_set_applies
			WHERE project_id=$1 AND machine_id=$2 AND kind=$3 ORDER BY applied_at DESC LIMIT 1`,
			projectID, machineID, kind).Scan(&ok); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			return 0, 0, e
		}
		required++
		if ok {
			verified++
		}
	}
	return required, verified, nil
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
