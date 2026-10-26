package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
)

type AgentNetworkInventory struct {
	Version       string                     `json:"version"`
	MachineID     string                     `json:"machine_id"`
	CredentialID  string                     `json:"credential_id"`
	Source        string                     `json:"source"`
	Enabled       bool                       `json:"enabled"`
	State         string                     `json:"state"`
	Interfaces    []agentinventory.Interface `json:"interfaces"`
	FailureCode   string                     `json:"failure_code,omitempty"`
	CollectedAt   *time.Time                 `json:"collected_at,omitempty"`
	ReceivedAt    *time.Time                 `json:"received_at,omitempty"`
	LastSuccessAt *time.Time                 `json:"last_success_at,omitempty"`
	TTLSeconds    int                        `json:"ttl_seconds"`
}

func (s *Store) recordAgentNetworkInventoryTx(ctx context.Context, tx *sql.Tx, credential *AgentCredential, connection, certHash string, sequence uint64, frame *agentinventory.NetworkFrame) error {
	if frame == nil {
		// Legacy/disabled heartbeats do not allocate an inventory row. If a prior
		// opt-in existed, preserve its observations but explicitly mark it disabled.
		_, err := tx.ExecContext(ctx, `UPDATE agent_network_inventory SET connection_id=$2,frame_sequence=$3,certificate_hash=$4,state='disabled',failure_code='',received_at=clock_timestamp() WHERE credential_id=$1`, credential.ID, connection, int64(sequence), certHash)
		return err
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if frame.Validate(now) != nil {
		return ErrAgentDenied
	}
	var oldConnection string
	var baseline sql.NullString
	var collected, success sql.NullTime
	raw := []byte(`[]`)
	err := tx.QueryRowContext(ctx, `SELECT connection_id,baseline_connection_id,interfaces,collected_at,last_success_at FROM agent_network_inventory WHERE credential_id=$1 FOR UPDATE`, credential.ID).Scan(&oldConnection, &baseline, &raw, &collected, &success)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if frame.State == "ready" {
		var previous []agentinventory.Interface
		if !frame.Full {
			if !baseline.Valid || baseline.String != connection || oldConnection != connection || json.Unmarshal(raw, &previous) != nil {
				return ErrAgentDenied
			}
		}
		merged, err := agentinventory.MergeNetwork(previous, *frame)
		if err != nil {
			return ErrAgentDenied
		}
		raw, err = json.Marshal(merged)
		if err != nil {
			return err
		}
		baseline = sql.NullString{String: connection, Valid: true}
		collected = sql.NullTime{Time: frame.CapturedAt, Valid: true}
		success = sql.NullTime{Time: now, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_network_inventory(credential_id,project_id,machine_id,connection_id,baseline_connection_id,frame_sequence,certificate_hash,state,interfaces,failure_code,collected_at,last_success_at,received_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(credential_id) DO UPDATE SET connection_id=EXCLUDED.connection_id,baseline_connection_id=EXCLUDED.baseline_connection_id,frame_sequence=EXCLUDED.frame_sequence,certificate_hash=EXCLUDED.certificate_hash,state=EXCLUDED.state,interfaces=EXCLUDED.interfaces,failure_code=EXCLUDED.failure_code,collected_at=EXCLUDED.collected_at,last_success_at=EXCLUDED.last_success_at,received_at=EXCLUDED.received_at`, credential.ID, credential.ProjectID, credential.MachineID, connection, baseline, int64(sequence), certHash, frame.State, raw, frame.FailureCode, collected, success, now)
	return err
}

func (s *Store) GetAgentNetworkInventory(ctx context.Context, a *AuthSession, project, machine string) (*AgentNetworkInventory, error) {
	if !sshTrustIDPattern.MatchString(machine) {
		return nil, ErrNotFound
	}
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project); err != nil {
		return nil, err
	}
	credential, err := s.ActiveAgentIdentityTx(ctx, tx, project, machine)
	if errors.Is(err, ErrAgentDenied) {
		return nil, apiError(409, "AGENT_IDENTITY_REQUIRED", "Current Agent identity and provisioned asset are required")
	}
	if err != nil {
		return nil, err
	}
	out := &AgentNetworkInventory{Version: agentinventory.NetworkVersion, MachineID: machine, CredentialID: credential.ID, Source: "agent", Enabled: s.AgentNetworkInventoryEnabled, State: "disabled", Interfaces: []agentinventory.Interface{}, TTLSeconds: 30}
	var raw []byte
	var collected, received, success sql.NullTime
	var live bool
	var now time.Time
	err = tx.QueryRowContext(ctx, `SELECT i.state,i.interfaces,i.failure_code,i.collected_at,i.received_at,i.last_success_at,c.disconnected_at IS NULL AND c.last_seen_at>now()-interval '30 seconds',now() FROM agent_network_inventory i JOIN agent_connections c ON c.id=i.connection_id WHERE i.credential_id=$1 AND i.project_id=$2 AND i.machine_id=$3`, credential.ID, project, machine).Scan(&out.State, &raw, &out.FailureCode, &collected, &received, &success, &live, &now)
	if errors.Is(err, sql.ErrNoRows) {
		return out, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &out.Interfaces) != nil {
		return nil, ErrAgentDenied
	}
	if collected.Valid {
		out.CollectedAt = &collected.Time
	}
	if received.Valid {
		out.ReceivedAt = &received.Time
	}
	if success.Valid {
		out.LastSuccessAt = &success.Time
	}
	if !out.Enabled {
		out.State = "disabled"
	} else if out.State == "ready" && (!live || !collected.Valid || !success.Valid || !now.Before(collected.Time.Add(30*time.Second)) || !now.Before(success.Time.Add(30*time.Second))) {
		out.State = "stale"
	}
	return out, tx.Commit()
}
