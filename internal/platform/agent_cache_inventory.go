package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
)

type AgentCacheInventory struct {
	Version         string                     `json:"version"`
	MachineID       string                     `json:"machine_id"`
	CredentialID    string                     `json:"credential_id"`
	Source          string                     `json:"source"`
	Enabled         bool                       `json:"enabled"`
	State           string                     `json:"state"`
	ContentVerified bool                       `json:"content_verified"`
	ManifestSHA256  string                     `json:"manifest_sha256,omitempty"`
	Entries         []agentinventory.CacheItem `json:"entries"`
	FailureCode     string                     `json:"failure_code,omitempty"`
	CollectedAt     *time.Time                 `json:"collected_at,omitempty"`
	ReceivedAt      *time.Time                 `json:"received_at,omitempty"`
	LastSuccessAt   *time.Time                 `json:"last_success_at,omitempty"`
	TTLSeconds      int                        `json:"ttl_seconds"`
}

func (s *Store) recordAgentCacheInventoryTx(ctx context.Context, tx *sql.Tx, credential *AgentCredential, connection, certHash string, sequence uint64, frame *agentinventory.CacheFrame) error {
	if frame == nil {
		_, err := tx.ExecContext(ctx, `UPDATE agent_cache_inventory SET connection_id=$2,frame_sequence=$3,certificate_hash=$4,state='disabled',failure_code='',received_at=clock_timestamp() WHERE credential_id=$1`, credential.ID, connection, int64(sequence), certHash)
		return err
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return err
	}
	if frame.Validate(now) != nil {
		return ErrAgentDenied
	}
	raw := []byte(`[]`)
	manifest := ""
	var collected, success sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT entries,manifest_sha256,collected_at,last_success_at FROM agent_cache_inventory WHERE credential_id=$1 FOR UPDATE`, credential.ID).Scan(&raw, &manifest, &collected, &success)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if frame.State == "ready" {
		raw, err = json.Marshal(frame.Entries)
		if err != nil {
			return err
		}
		manifest = frame.ManifestSHA256
		collected = sql.NullTime{Time: frame.CapturedAt, Valid: true}
		success = sql.NullTime{Time: now, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_cache_inventory(credential_id,project_id,machine_id,connection_id,frame_sequence,certificate_hash,state,manifest_sha256,entries,failure_code,collected_at,last_success_at,received_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(credential_id) DO UPDATE SET connection_id=EXCLUDED.connection_id,frame_sequence=EXCLUDED.frame_sequence,certificate_hash=EXCLUDED.certificate_hash,state=EXCLUDED.state,manifest_sha256=EXCLUDED.manifest_sha256,entries=EXCLUDED.entries,failure_code=EXCLUDED.failure_code,collected_at=EXCLUDED.collected_at,last_success_at=EXCLUDED.last_success_at,received_at=EXCLUDED.received_at`, credential.ID, credential.ProjectID, credential.MachineID, connection, int64(sequence), certHash, frame.State, manifest, raw, frame.FailureCode, collected, success, now)
	return err
}

func (s *Store) GetAgentCacheInventory(ctx context.Context, a *AuthSession, project, machine string) (*AgentCacheInventory, error) {
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
	out := &AgentCacheInventory{Version: agentinventory.CacheVersion, MachineID: machine, CredentialID: credential.ID, Source: "agent", Enabled: s.TaskCacheInventoryEnabled, State: "disabled", Entries: []agentinventory.CacheItem{}, TTLSeconds: 30}
	var raw []byte
	var collected, received, success sql.NullTime
	var live bool
	var now time.Time
	err = tx.QueryRowContext(ctx, `SELECT i.state,i.manifest_sha256,i.entries,i.failure_code,i.collected_at,i.received_at,i.last_success_at,c.disconnected_at IS NULL AND c.last_seen_at>now()-interval '30 seconds',now() FROM agent_cache_inventory i JOIN agent_connections c ON c.id=i.connection_id WHERE i.credential_id=$1 AND i.project_id=$2 AND i.machine_id=$3`, credential.ID, project, machine).Scan(&out.State, &out.ManifestSHA256, &raw, &out.FailureCode, &collected, &received, &success, &live, &now)
	if errors.Is(err, sql.ErrNoRows) {
		return out, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &out.Entries) != nil {
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
