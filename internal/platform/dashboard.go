package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Gate dashboard (WP-2.8, FR-4 / DESIGN-SYSTEM §4.4). One read aggregates
// the six gate cards and the project KPIs. G1, G4 and G5 read real evidence
// in the mock channel (G1 the gate evidence rows, G4 the WP-3.2 read-back
// receipts, G5 the WP-3.3 connectivity conclusions); the rest report their
// honest not-implemented state instead of a fabricated pass. The KPIs count open issues and trust states from the
// existing tables.

type GateCard struct {
	Gate             string    `json:"gate"`
	Title            string    `json:"title"`
	State            string    `json:"state"` // pass | fail | waived | not_implemented | no_evidence
	Summary          string    `json:"summary"`
	PassRate         int       `json:"pass_rate"`
	LatestAt         time.Time `json:"latest_at,omitempty"`
	LatestConclusion string    `json:"latest_conclusion,omitempty"`
	// Layers is the G5 five-layer pass-rate breakdown (DS 4.4 mini bar).
	Layers []GateLayerRate `json:"layers,omitempty"`
}

// GateLayerRate is one L1-L5 layer of the newest concluded connectivity
// run: planned pairs from the frozen plan, measured/passed/failed from the
// per-pair results, and the pass percentage of the plan.
type GateLayerRate struct {
	Layer    string `json:"layer"`
	Planned  int    `json:"planned"`
	Measured int    `json:"measured"`
	Passed   int    `json:"passed"`
	Failed   int    `json:"failed"`
	Rate     int    `json:"rate"`
}

type DashboardKPIs struct {
	AssetsTotal      int `json:"assets_total"`
	TrustEstablished int `json:"trust_established"`
	TrustUnverified  int `json:"trust_unverified"`
	TrustRevoked     int `json:"trust_revoked"`
	OpenIssues       int `json:"open_issues"`
	InRepairIssues   int `json:"in_repair_issues"`
	LldpOpenDiffs    int `json:"lldp_open_diffs"`
}

type GateDashboard struct {
	Cards GateCards     `json:"cards"`
	KPIs  DashboardKPIs `json:"kpis"`
}

type GateCards struct {
	G1 GateCard `json:"G1"`
	G2 GateCard `json:"G2"`
	G3 GateCard `json:"G3"`
	G4 GateCard `json:"G4"`
	G5 GateCard `json:"G5"`
	G6 GateCard `json:"G6"`
}

func gateCardMeta(gate string) (title, summary string) {
	switch gate {
	case "G1":
		return "预收集完整性", "采集字段完整率 100%、MAC 与 BOM 一致"
	case "G2":
		return "LLDP 线序验证", "LLDP ↔ 交换机 ↔ BOM 三方一致"
	case "G3":
		return "映射发布", "不可变映射版本发布（G2 后）"
	case "G4":
		return "基线配置", "四类配置渲染读回一致"
	case "G5":
		return "RoCE 连通性", "五层连通 + RDMA 带宽达阈"
	case "G6":
		return "NCCL 报告评审", "集群测试报告达标 + 调优清单确认"
	}
	return gate, ""
}

// GetGateDashboard aggregates the board. G1 reads the newest evidence rows
// per gate across the project (any target set): the latest conclusion wins
// for the badge, and the pass rate counts PASS among the recent window.
func (s *Store) GetGateDashboard(ctx context.Context, a *AuthSession, project *ProjectSummary) (*GateDashboard, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	dashboard := &GateDashboard{Cards: GateCards{}, KPIs: DashboardKPIs{}}
	for _, gate := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		card := GateCard{Gate: gate, State: "not_implemented", PassRate: 0}
		card.Title, card.Summary = gateCardMeta(gate)
		if gate == "G4" {
			g4, e := s.g4Card(ctx, tx, project.ID, card)
			if e != nil {
				return nil, e
			}
			setCard(&dashboard.Cards, gate, g4)
			continue
		}
		if gate == "G5" {
			g5, e := s.g5Card(ctx, tx, project.ID, card)
			if e != nil {
				return nil, e
			}
			setCard(&dashboard.Cards, gate, g5)
			continue
		}
		if gate != "G1" {
			// Only G1 has a judge in the mock channel; anything else is honest
			// about not being implemented (never fabricates a pass).
			switch gate {
			case "G2":
				card.State = "no_evidence"
			default:
				card.State = "not_implemented"
			}
			setCard(&dashboard.Cards, gate, card)
			continue
		}
		var conclusion string
		var createdAt time.Time
		e := tx.QueryRowContext(ctx, `SELECT conclusion, created_at FROM gate_evidences
			WHERE project_id=$1 AND gate='G1' ORDER BY created_at DESC LIMIT 1`, project.ID).Scan(&conclusion, &createdAt)
		switch {
		case e == nil:
			card.LatestAt, card.LatestConclusion = createdAt, conclusion
			switch conclusion {
			case "PASS":
				card.State, card.PassRate = "pass", 100
			case "FAIL":
				card.State, card.PassRate = "fail", 0
			case "WAIVED":
				card.State, card.PassRate = "waived", 0
			}
		default:
			// No G1 evidence at all.
			card.State = "no_evidence"
		}
		setCard(&dashboard.Cards, gate, card)
	}
	// KPIs: trust states and open issues.
	if e = tx.QueryRowContext(ctx, `SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE m.ssh_trust='established'),
			COUNT(*) FILTER (WHERE m.ssh_trust='unverified'),
			COUNT(*) FILTER (WHERE m.ssh_trust='revoked')
		FROM project_assets pa JOIN machines m ON m.id=pa.machine_id
		WHERE pa.project_id=$1`, project.ID).Scan(
		&dashboard.KPIs.AssetsTotal, &dashboard.KPIs.TrustEstablished, &dashboard.KPIs.TrustUnverified, &dashboard.KPIs.TrustRevoked); e != nil {
		return nil, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT
			COUNT(*) FILTER (WHERE status IN('open','assigned')),
			COUNT(*) FILTER (WHERE status='in_repair')
		FROM issues WHERE project_id=$1`, project.ID).Scan(&dashboard.KPIs.OpenIssues, &dashboard.KPIs.InRepairIssues); e != nil {
		return nil, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM lldp_differences WHERE project_id=$1 AND resolved_at IS NULL", project.ID).Scan(&dashboard.KPIs.LldpOpenDiffs); e != nil {
		return nil, e
	}
	return dashboard, nil
}

// g4Card (FR-4 G4, PRD 3.4) reads the WP-3.1/3.2 read-back evidence: a
// machine counts as aligned when its newest apply per required renderer kind
// is verified — the sampled read-back matched the rendered plan. Required
// kinds follow the machine's active roles (a storage-only target never needs
// the GPU-NIC binding), and an apply already implies a published mapping
// (G3), so the precondition is enforced at write time.
func (s *Store) g4Card(ctx context.Context, tx *sql.Tx, projectID string, card GateCard) (GateCard, error) {
	type newest struct {
		verified bool
	}
	perMachine := map[string]map[string]newest{}
	rows, e := tx.QueryContext(ctx, `SELECT machine_id, kind, verified FROM (
			SELECT DISTINCT ON (machine_id, kind) machine_id, kind, verified
			FROM config_set_applies WHERE project_id=$1 ORDER BY machine_id, kind, applied_at DESC
		) newest`, projectID)
	if e != nil {
		return card, e
	}
	for rows.Next() {
		var machineID, kind string
		var verified bool
		if e = rows.Scan(&machineID, &kind, &verified); e != nil {
			rows.Close()
			return card, e
		}
		if perMachine[machineID] == nil {
			perMachine[machineID] = map[string]newest{}
		}
		perMachine[machineID][kind] = newest{verified: verified}
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return card, e
	}
	rows.Close()
	if len(perMachine) == 0 {
		card.State = "no_evidence"
		return card, nil
	}
	total, passed, failed, unaligned := 0, 0, 0, 0
	for machineID, kinds := range perMachine {
		required := g4RequiredKinds(ctx, tx, projectID, machineID)
		for _, kind := range required {
			total++
			entry, ok := kinds[kind]
			if !ok {
				unaligned++
				continue
			}
			if entry.verified {
				passed++
			} else {
				failed++
			}
		}
	}
	switch {
	case failed > 0:
		card.State = "fail"
	case unaligned > 0 || total == 0:
		// Applies exist and every sampled read-back matched, but a target has
		// not pulled all of its required configuration yet: no complete G4
		// evidence, and the card says so instead of claiming a pass.
		card.State = "no_evidence"
	default:
		card.State = "pass"
	}
	if total > 0 {
		card.PassRate = passed * 100 / total
	}
	return card, nil
}

// g4RequiredKinds derives the renderer kinds a target must carry from its
// active roles: every active rail needs the network plan and the partition,
// a compute rail additionally needs the GPU-NIC binding, and the RoCE
// parameters ride the same rails.
func g4RequiredKinds(ctx context.Context, tx *sql.Tx, projectID, machineID string) []string {
	var active, compute int
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE role='compute')
		FROM physical_ports WHERE machine_id=$1 AND role<>'unused'`, machineID).Scan(&active, &compute); e != nil {
		return nil
	}
	if active == 0 {
		return nil
	}
	kinds := []string{"gpu_network", "nic_partition", "roce_import"}
	if compute > 0 {
		kinds = append(kinds, "gpu_nic_binding")
	}
	return kinds
}

// g5Card judges the RoCE connectivity gate: the newest G5 evidence decides
// the badge, and the run that evidence was written for provides the L1-L5
// layer pass-rate breakdown. A missing gate is never a pass.
func (s *Store) g5Card(ctx context.Context, tx *sql.Tx, projectID string, card GateCard) (GateCard, error) {
	var conclusion, detail string
	var createdAt time.Time
	e := tx.QueryRowContext(ctx, `SELECT conclusion, created_at, detail FROM gate_evidences
		WHERE project_id=$1 AND gate='G5' ORDER BY created_at DESC LIMIT 1`, projectID).Scan(&conclusion, &createdAt, &detail)
	switch {
	case e == nil:
		card.LatestAt, card.LatestConclusion = createdAt, conclusion
		switch conclusion {
		case "PASS":
			card.State, card.PassRate = "pass", 100
		case "FAIL":
			card.State, card.PassRate = "fail", 0
		case "WAIVED":
			card.State, card.PassRate = "waived", 0
		default:
			card.State = "no_evidence"
		}
	case errors.Is(e, sql.ErrNoRows):
		card.State = "no_evidence"
	default:
		return card, e
	}
	runID := ""
	if detail != "" {
		var parsed map[string]any
		if json.Unmarshal([]byte(detail), &parsed) == nil {
			if value, ok := parsed["run_id"].(string); ok {
				runID = value
			}
		}
	}
	if runID == "" {
		// The evidence is written in the finish transaction, so the newest
		// finished run is the same run in practice.
		if e := tx.QueryRowContext(ctx, `SELECT id FROM nettest_runs
			WHERE project_id=$1 AND state='finished' ORDER BY finished_at DESC LIMIT 1`, projectID).Scan(&runID); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return card, e
		}
	}
	if runID == "" {
		return card, nil
	}
	var layersJSON []byte
	if e := tx.QueryRowContext(ctx, `SELECT layers FROM nettest_runs WHERE id=$1`, runID).Scan(&layersJSON); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return card, nil
		}
		return card, e
	}
	plan := []NettestLayerPlan{}
	if e := json.Unmarshal(layersJSON, &plan); e != nil {
		return card, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT layer,
			COUNT(*) FILTER (WHERE state='pass'), COUNT(*) FILTER (WHERE state='fail')
		FROM nettest_pairs WHERE run_id=$1 GROUP BY layer`, runID)
	if e != nil {
		return card, e
	}
	outcomes := map[string][2]int{}
	for rows.Next() {
		var layer string
		var passed, failed int
		if e = rows.Scan(&layer, &passed, &failed); e != nil {
			rows.Close()
			return card, e
		}
		outcomes[layer] = [2]int{passed, failed}
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return card, e
	}
	rows.Close()
	layers := []GateLayerRate{}
	for _, entry := range plan {
		rate := GateLayerRate{Layer: entry.Layer, Planned: entry.Pairs}
		if outcome, ok := outcomes[entry.Layer]; ok {
			rate.Passed, rate.Failed = outcome[0], outcome[1]
			rate.Measured = outcome[0] + outcome[1]
		}
		if rate.Planned > 0 {
			rate.Rate = rate.Passed * 100 / rate.Planned
		}
		layers = append(layers, rate)
	}
	card.Layers = layers
	return card, nil
}

func setCard(cards *GateCards, gate string, card GateCard) {
	switch gate {
	case "G1":
		cards.G1 = card
	case "G2":
		cards.G2 = card
	case "G3":
		cards.G3 = card
	case "G4":
		cards.G4 = card
	case "G5":
		cards.G5 = card
	case "G6":
		cards.G6 = card
	}
}
