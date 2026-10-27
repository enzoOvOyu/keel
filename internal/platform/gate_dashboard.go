package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type GateDashboardCard struct {
	WaiverID         string     `json:"waiver_id"`
	Gate             string     `json:"gate"`
	OriginalVerdict  string     `json:"original_verdict"`
	EffectiveVerdict string     `json:"effective_verdict"`
	EvidenceID       string     `json:"evidence_id"`
	ScopeHash        string     `json:"scope_hash"`
	ObservedAt       *time.Time `json:"observed_at"`
	ValidUntil       *time.Time `json:"valid_until"`
	Origin           string     `json:"origin"`
	Reason           string     `json:"reason"`
}
type GateDashboardAsset struct {
	AssetID   string               `json:"asset_id"`
	MachineID string               `json:"machine_id"`
	Hostname  string               `json:"hostname"`
	State     string               `json:"state"`
	Cards     []GateDashboardCard  `json:"cards"`
	Network   PortMonitorFleetItem `json:"network"`
}
type GateDashboardKPI struct {
	Assets          int `json:"assets"`
	NetworkNormal   int `json:"network_normal"`
	NetworkAbnormal int `json:"network_abnormal"`
	NetworkUnknown  int `json:"network_unknown"`
	Maintained      int `json:"maintained"`
	OpenIssues      int `json:"open_issues"`
	TrustAnomalies  int `json:"trust_anomalies"`
}
type GateDashboard struct {
	ProjectID string               `json:"project_id"`
	AsOf      time.Time            `json:"as_of"`
	KPI       GateDashboardKPI     `json:"kpi"`
	Items     []GateDashboardAsset `json:"items"`
}

// P2 implements actual G1-G3 consumers. G4-G6 stay explicitly unavailable until
// their P3 domains exist; lifecycle labels never synthesize successful evidence.
func (s *Store) GateDashboard(ctx context.Context, a *AuthSession, project string) (*GateDashboard, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	out := &GateDashboard{ProjectID: project, AsOf: time.Now().UTC(), Items: []GateDashboardAsset{}}
	rows, e := tx.QueryContext(ctx, `SELECT id,machine_id,hostname,state FROM project_assets WHERE project_id=$1 ORDER BY hostname,id LIMIT 201`, project)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var item GateDashboardAsset
		if e = rows.Scan(&item.AssetID, &item.MachineID, &item.Hostname, &item.State); e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(out.Items) > 200 {
		return nil, apiError(409, "GATE_DASHBOARD_SCOPE_LIMIT", "Dashboard scope exceeds the current 200 logical asset delivery boundary")
	}
	out.KPI.Assets = len(out.Items)
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM issues WHERE project_id=$1 AND state<>'closed'`, project).Scan(&out.KPI.OpenIssues); e != nil {
		return nil, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assets asset JOIN machines machine ON machine.id=asset.machine_id WHERE asset.project_id=$1 AND asset.state<>'maintained' AND(machine.ssh_trust<>'established' OR machine.ssh_trust_source<>'ssh')`, project).Scan(&out.KPI.TrustAnomalies); e != nil {
		return nil, e
	}
	for i := range out.Items {
		item := &out.Items[i]
		item.Cards, e = s.gateDashboardCardsTx(ctx, tx, project, item.AssetID)
		if e != nil {
			return nil, e
		}
		network, e := s.portMonitorSnapshotTx(ctx, tx, project, item.AssetID)
		if e != nil {
			return nil, e
		}
		item.Network = PortMonitorFleetItem{AssetID: item.AssetID, MappingState: network.MappingState, MappingValidUntil: network.MappingValidUntil, MappingRevisionID: network.MappingRevisionID, MappingReason: network.MappingReason, NetworkState: network.NetworkState, Summary: network.Summary}
		switch network.NetworkState {
		case "normal":
			out.KPI.NetworkNormal++
		case "abnormal":
			out.KPI.NetworkAbnormal++
		case "maintained":
			out.KPI.Maintained++
		default:
			out.KPI.NetworkUnknown++
		}
	}
	return out, tx.Commit()
}
func (s *Store) gateDashboardCardsTx(ctx context.Context, tx *sql.Tx, project, asset string) ([]GateDashboardCard, error) {
	cards := make([]GateDashboardCard, 6)
	for i, gate := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		cards[i] = GateDashboardCard{Gate: gate, EffectiveVerdict: "BLOCKED", Origin: "unknown", Reason: "GATE_EVIDENCE_REQUIRED"}
		if i >= 3 {
			cards[i].Reason = "P3_GATE_DOMAIN_NOT_IMPLEMENTED"
		}
	}
	g1, e := s.currentG1Tx(ctx, tx, project, asset)
	if e != nil {
		return nil, e
	}
	cards[0].EffectiveVerdict, cards[0].Reason = g1.EffectiveVerdict, g1.Reason
	if g1.Evidence != nil {
		proof := g1.Evidence
		card := &cards[0]
		card.WaiverID = proof.WaiverID
		card.OriginalVerdict = proof.Verdict
		card.EvidenceID = proof.ID
		card.ScopeHash = proof.ScopeHash
		card.ObservedAt = &proof.ObservedAt
		card.ValidUntil = &proof.ValidUntil
		if proof.EffectiveValidUntil != nil {
			card.ValidUntil = proof.EffectiveValidUntil
		}
		if e = tx.QueryRowContext(ctx, `SELECT origin FROM validation_precollect_records WHERE id=$1`, proof.PrecollectID).Scan(&card.Origin); e != nil {
			return nil, e
		}
	}
	var confirmationID string
	e = tx.QueryRowContext(ctx, `SELECT id FROM validation_wiring_confirmations WHERE project_id=$1 AND asset_id=$2 ORDER BY created_at DESC,id DESC LIMIT 1`, project, asset).Scan(&confirmationID)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e == nil {
		proof, e := s.wiringConfirmationTx(ctx, tx, project, confirmationID)
		if e != nil {
			return nil, e
		}
		cards[1] = GateDashboardCard{Gate: "G2", OriginalVerdict: "PASS", EffectiveVerdict: proof.EffectiveVerdict, EvidenceID: proof.ID, ScopeHash: proof.ScopeHash, ObservedAt: &proof.CreatedAt, ValidUntil: &proof.ValidUntil, Origin: "real", Reason: proof.InvalidationReason}
	}
	revision, e := scanMappingRevision(tx.QueryRowContext(ctx, `SELECT `+mappingRevisionFields+` FROM mapping_revisions r JOIN mapping_assessments a ON a.id=r.assessment_id WHERE r.project_id=$1 AND r.asset_id=$2 ORDER BY r.version DESC LIMIT 1`, project, asset))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return nil, e
	}
	if e == nil {
		if e = s.mappingValidityTx(ctx, tx, revision); e != nil {
			return nil, e
		}
		cards[2] = GateDashboardCard{Gate: "G3", OriginalVerdict: "PASS", EffectiveVerdict: revision.EffectiveVerdict, EvidenceID: revision.ID, ScopeHash: revision.Candidate.ScopeHash, ObservedAt: &revision.CreatedAt, ValidUntil: revision.Candidate.ValidUntil, Origin: "real", Reason: revision.InvalidationReason}
	}
	return cards, nil
}
