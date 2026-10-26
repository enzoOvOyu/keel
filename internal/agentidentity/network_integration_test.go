package agentidentity_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestAgentNetworkInventoryMTLSPostgres(t *testing.T) {
	f := newIdentityFixture(t, false, false, true)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	machine, serial := f.target(t)
	credential, pair := f.enroll(t, machine, serial)
	stream, err := f.client(t, &pair).Heartbeats(ctx)
	requireNoError(t, err)
	defer stream.CloseSend()
	message := &pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID()}
	send := func(frame *agentinventory.NetworkFrame) {
		t.Helper()
		message.Sequence++
		message.NetworkInventoryJson = nil
		if frame != nil {
			raw, err := json.Marshal(frame)
			requireNoError(t, err)
			message.NetworkInventoryJson = raw
		}
		requireNoError(t, stream.Send(message))
		ack, err := stream.Recv()
		requireNoError(t, err)
		if ack.GetSequence() != message.Sequence {
			t.Fatal("inventory heartbeat sequence changed")
		}
	}
	read := func() *platform.AgentNetworkInventory {
		t.Helper()
		out, err := f.store.GetAgentNetworkInventory(ctx, f.actor, f.project, machine)
		requireNoError(t, err)
		return out
	}
	a := agentinventory.Interface{Index: 1, Name: "eth0", MAC: "02:00:00:00:00:01", MTU: 1500, AdminUp: true, OperState: "up"}
	b := agentinventory.Interface{Index: 2, Name: "eth1", MAC: "02:00:00:00:00:02", MTU: 9000, OperState: "down"}
	full := agentinventory.NetworkDelta(nil, []agentinventory.Interface{a, b}, time.Now())
	send(&full)
	if got := read(); got.State != "ready" || len(got.Interfaces) != 2 || got.CredentialID != credential.ID || got.Source != "agent" {
		t.Fatal("authenticated inventory missing", got)
	}
	a.OperState = "down"
	delta := agentinventory.NetworkDelta([]agentinventory.Interface{full.Interfaces[0], full.Interfaces[1]}, []agentinventory.Interface{a}, time.Now())
	send(&delta)
	if got := read(); got.State != "ready" || len(got.Interfaces) != 1 || got.Interfaces[0].OperState != "down" {
		t.Fatal("delta did not update/remove", got)
	}
	failed := agentinventory.NetworkFrame{Version: agentinventory.NetworkVersion, State: "unavailable", CapturedAt: time.Now(), FailureCode: "interface_read_failed"}
	send(&failed)
	if got := read(); got.State != "unavailable" || len(got.Interfaces) != 1 || got.CollectedAt == nil {
		t.Fatal("collection failure erased history or became ready", got)
	}
	send(nil)
	if got := read(); got.State != "disabled" || len(got.Interfaces) != 1 {
		t.Fatal("opt-out erased observations or remained ready", got)
	}
	full = agentinventory.NetworkDelta(nil, []agentinventory.Interface{a}, time.Now())
	send(&full)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE agent_network_inventory SET collected_at=now()-interval '31 seconds' WHERE credential_id=$1`, credential.ID)
	requireNoError(t, err)
	if got := read(); got.State != "stale" {
		t.Fatal("stale inventory presented as ready", got)
	}
	f.store.AgentNetworkInventoryEnabled = false
	if got := read(); got.Enabled || got.State != "disabled" {
		t.Fatal("API opt-out still showed ready", got)
	}
}

func TestAgentNetworkDeltaRequiresConnectionBaseline(t *testing.T) {
	f := newIdentityFixture(t, false, false, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	machine, serial := f.target(t)
	_, pair := f.enroll(t, machine, serial)
	stream, err := f.client(t, &pair).Heartbeats(ctx)
	requireNoError(t, err)
	defer stream.CloseSend()
	frame := agentinventory.NetworkDelta([]agentinventory.Interface{}, []agentinventory.Interface{}, time.Now())
	raw, err := json.Marshal(frame)
	requireNoError(t, err)
	requireNoError(t, stream.Send(&pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID(), Sequence: 1, NetworkInventoryJson: raw}))
	_, err = stream.Recv()
	requireDenied(t, err)
	var rows int
	requireNoError(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM agent_network_inventory WHERE machine_id=$1`, machine).Scan(&rows))
	if rows != 0 {
		t.Fatal("delta without baseline persisted partial observations")
	}
}
