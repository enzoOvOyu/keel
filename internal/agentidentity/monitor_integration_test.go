package agentidentity_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/platform"
)

// Only software identities and an isolated database: never a hardware gate proof.
func TestMonitorPostgresMTLSDurableReplayPolicyHistoryAndStageTransition(t *testing.T) {
	f := newIdentityFixture(t)
	f.store.MonitorEnabled = true
	machine, serial := f.target(t)
	credential, pair := f.enroll(t, machine, serial)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream, err := f.client(t, &pair).Heartbeats(ctx)
	requireNoError(t, err)
	in := &pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID()}
	send := func(raw []byte) *pb.HeartbeatAck {
		t.Helper()
		in.Sequence++
		in.TelemetryJson = raw
		requireNoError(t, stream.Send(in))
		ack, e := stream.Recv()
		requireNoError(t, e)
		if ack.Sequence != in.Sequence {
			t.Fatal("heartbeat sequence changed")
		}
		return ack
	}
	decodePolicy := func(ack *pb.HeartbeatAck) monitor.Assignment {
		t.Helper()
		var assignment monitor.Assignment
		requireNoError(t, json.Unmarshal(ack.CollectPolicyJson, &assignment))
		requireNoError(t, assignment.Policy.Validate())
		return assignment
	}
	initial := decodePolicy(send(nil))
	if initial.Policy.Stage != "M1" {
		t.Fatal("wrong initial stage")
	}
	value := 1.25
	now := time.Now().UTC().Add(-time.Second)
	frame := monitor.Frame{Version: monitor.Version, ID: "first-frame", PolicyID: initial.PolicyID, Full: true, Samples: []monitor.Sample{{Key: "host.load1", Entity: "host", Value: &value, Quality: "good", ObservedAt: now}}, Unchanged: []monitor.Unchanged{}}
	raw, err := json.Marshal(frame)
	requireNoError(t, err)
	if send(raw).TelemetryFrameId != frame.ID || send(raw).TelemetryFrameId != frame.ID {
		t.Fatal("durable replay ACK lost")
	}
	var count int
	requireNoError(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM monitor_metric_samples WHERE credential_id=$1`, credential.ID).Scan(&count))
	if count != 1 {
		t.Fatal("replay duplicated samples", count)
	}
	policy := monitor.DefaultPolicy("M1")
	policy.Intervals["D3"] = 120
	input := platform.MonitorPolicyInput{Policy: policy, Reason: "software policy history", ExpectedVersion: 0}
	key := platform.RandomID()
	revision, err := f.store.SaveMonitorPolicy(ctx, f.actor, f.project, input, key, "monitor-test")
	requireNoError(t, err)
	replay, err := f.store.SaveMonitorPolicy(ctx, f.actor, f.project, input, key, "monitor-test")
	requireNoError(t, err)
	if replay.ID != revision.ID {
		t.Fatal("policy retry created new revision")
	}
	if _, err = f.store.SaveMonitorPolicy(ctx, f.actor, f.project, input, platform.RandomID(), "monitor-test"); err == nil {
		t.Fatal("stale policy version accepted")
	}
	if assignment := decodePolicy(send(nil)); assignment.PolicyID != revision.ID {
		t.Fatal("new policy not assigned")
	}
	// A queued delta carries its original issued policy after a policy switch.
	frame = monitor.Frame{Version: monitor.Version, ID: "queued-delta", PolicyID: initial.PolicyID, BaselineID: "first-frame", Samples: []monitor.Sample{}, Unchanged: []monitor.Unchanged{{Key: "host.load1", Entity: "host", ObservedAt: now.Add(time.Millisecond)}}}
	raw, err = json.Marshal(frame)
	requireNoError(t, err)
	if send(raw).TelemetryFrameId != frame.ID {
		t.Fatal("queued historical policy frame rejected")
	}
	var interval int
	requireNoError(t, f.store.DB.QueryRowContext(ctx, `SELECT interval_seconds FROM monitor_metric_samples WHERE credential_id=$1 ORDER BY sequence DESC LIMIT 1`, credential.ID).Scan(&interval))
	if interval != 600 {
		t.Fatal("queued sample rewritten under new intervals", interval)
	}
	// Stage advancement keeps the same certificate identity and changes policy.
	_, err = f.store.DB.ExecContext(ctx, `UPDATE project_assets SET state='lldp' WHERE project_id=$1 AND machine_id=$2`, f.project, machine)
	requireNoError(t, err)
	if assignment := decodePolicy(send(nil)); assignment.Policy.Stage != "M2" {
		t.Fatal("stage change disconnected identity or retained M1")
	}
	var asset string
	requireNoError(t, f.store.DB.QueryRowContext(ctx, `SELECT id FROM project_assets WHERE project_id=$1 AND machine_id=$2`, f.project, machine).Scan(&asset))
	snapshot, err := f.store.MonitorSnapshot(ctx, f.actor, f.project, asset, 0)
	requireNoError(t, err)
	if len(snapshot.Items) != 1 || snapshot.Items[0].Source != "host" || snapshot.Items[0].Origin != "real" || snapshot.Items[0].MappingRevisionID != "" {
		t.Fatal("telemetry fabricated mapping or source")
	}
	trend, err := f.store.MonitorTrend(ctx, f.actor, f.project, asset, "host.load1", "host", "host", "agent", "real", "1h")
	requireNoError(t, err)
	if len(trend.Items) == 0 {
		t.Fatal("missing gap-aware trend buckets")
	}
	// An ID is bound to exact bytes; mutated replay must never receive an ACK.
	frame.Unchanged[0].ObservedAt = now.Add(2 * time.Millisecond)
	raw, err = json.Marshal(frame)
	requireNoError(t, err)
	in.Sequence++
	in.TelemetryJson = raw
	requireNoError(t, stream.Send(in))
	_, err = stream.Recv()
	requireDenied(t, err)
}
