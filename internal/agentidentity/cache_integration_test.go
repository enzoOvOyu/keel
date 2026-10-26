package agentidentity

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestAgentCacheInventoryMTLSAndPostgres(t *testing.T) {
	f := newIdentityFixture(t)
	f.store.TaskCacheInventoryEnabled = true
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	machine, serial := f.target(t)
	credential, pair := f.enroll(t, machine, serial)
	stream, err := f.client(t, &pair).Heartbeats(ctx)
	requireNoError(t, err)
	defer stream.CloseSend()
	message := &pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID()}
	send := func(frame *agentinventory.CacheFrame) {
		t.Helper()
		message.Sequence++
		message.CacheInventoryJson = nil
		if frame != nil {
			raw, err := json.Marshal(frame)
			requireNoError(t, err)
			message.CacheInventoryJson = raw
		}
		requireNoError(t, stream.Send(message))
		ack, err := stream.Recv()
		requireNoError(t, err)
		if ack.GetSequence() != message.Sequence {
			t.Fatal("cache heartbeat sequence changed")
		}
	}
	read := func() *platform.AgentCacheInventory {
		t.Helper()
		out, err := f.store.GetAgentCacheInventory(ctx, f.actor, f.project, machine)
		requireNoError(t, err)
		return out
	}
	frame := agentinventory.CacheFrame{Version: agentinventory.CacheVersion, State: "ready", CapturedAt: time.Now(), ManifestSHA256: strings.Repeat("a", 64), Entries: []agentinventory.CacheItem{{SHA256: strings.Repeat("b", 64), Size: 17}}}
	send(&frame)
	if got := read(); got.State != "ready" || len(got.Entries) != 1 || got.ContentVerified || got.CredentialID != credential.ID || got.Source != "agent" {
		t.Fatal("cache ownership source/identity/proof incorrect", got)
	}
	failed := agentinventory.CacheFrame{Version: agentinventory.CacheVersion, State: "unavailable", CapturedAt: time.Now(), Entries: []agentinventory.CacheItem{}, FailureCode: "cache_index_unavailable"}
	send(&failed)
	if got := read(); got.State != "unavailable" || len(got.Entries) != 1 || got.LastSuccessAt == nil {
		t.Fatal("failure erased history or became ready", got)
	}
	send(nil)
	if got := read(); got.State != "disabled" || len(got.Entries) != 1 {
		t.Fatal("legacy/opt-out erased history", got)
	}
	frame.CapturedAt = time.Now()
	send(&frame)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE agent_cache_inventory SET collected_at=now()-interval '31 seconds' WHERE credential_id=$1`, credential.ID)
	requireNoError(t, err)
	if got := read(); got.State != "stale" {
		t.Fatal("stale index presented as ready", got)
	}
	f.store.TaskCacheInventoryEnabled = false
	if got := read(); got.Enabled || got.State != "disabled" {
		t.Fatal("feature opt-out still ready", got)
	}
}

func TestAgentCacheInvalidFrameRollsBackHeartbeatSequence(t *testing.T) {
	f := newIdentityFixture(t)
	f.store.TaskCacheInventoryEnabled = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	machine, serial := f.target(t)
	credential, pair := f.enroll(t, machine, serial)
	stream, err := f.client(t, &pair).Heartbeats(ctx)
	requireNoError(t, err)
	defer stream.CloseSend()
	requireNoError(t, stream.Send(&pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID(), Sequence: 1, CacheInventoryJson: []byte(`{"version":"agent-cache-inventory/v1","state":"ready","entries":[]}`)}))
	_, err = stream.Recv()
	requireDenied(t, err)
	var sequence int64
	requireNoError(t, f.store.DB.QueryRowContext(ctx, `SELECT sequence FROM agent_connections WHERE credential_id=$1`, credential.ID).Scan(&sequence))
	if sequence != 0 {
		t.Fatal("invalid cache frame advanced heartbeat", sequence)
	}
	var rows int
	requireNoError(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM agent_cache_inventory WHERE credential_id=$1`, credential.ID).Scan(&rows))
	if rows != 0 {
		t.Fatal("invalid index persisted partial state")
	}
}
