package execution

import (
	"context"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestHostFlowCircuitBreakerFreezeAndRetryPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	flow := publishHostFixture(t, f)
	in := HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}}
	firstKey := platform.RandomID()
	first, err := f.service.CreateHostFlow(ctx, f.actor, f.project, in, firstKey, "policy")
	mustAgent(t, err)
	if first.CircuitBreakerPercent != 5 || first.Snapshot.CircuitBreakerPolicy == nil || first.Snapshot.CircuitBreakerPolicy.Source != "built_in" {
		t.Fatal("unsafe default", first)
	}
	project, err := f.store.GetProject(ctx, f.actor, f.project)
	mustAgent(t, err)
	key, err := f.store.UpsertParamKey(ctx, f.actor, project, platform.UpsertParamKeyInput{Key: "exec.circuit_breaker_threshold", ValueType: "string", DefaultValue: "8"}, "policy")
	mustAgent(t, err)
	repeated, err := f.service.CreateHostFlow(ctx, f.actor, f.project, in, firstKey, "policy")
	mustAgent(t, err)
	if repeated.ID != first.ID || repeated.CircuitBreakerPercent != 5 {
		t.Fatal("retry recalculated original policy")
	}
	second, err := f.service.CreateHostFlow(ctx, f.actor, f.project, in, platform.RandomID(), "policy")
	mustAgent(t, err)
	if second.CircuitBreakerPercent != 8 || second.Snapshot.CircuitBreakerPolicy.ParamKeyID != key.ID {
		t.Fatal("project default not frozen", second)
	}
	explicit := in
	explicit.CircuitBreakerPercent = 11
	override, err := f.service.CreateHostFlow(ctx, f.actor, f.project, explicit, platform.RandomID(), "policy")
	mustAgent(t, err)
	if override.CircuitBreakerPercent != 11 || override.Snapshot.CircuitBreakerPolicy.DefaultPercent != 8 {
		t.Fatal("explicit/default policy not distinguished")
	}
	_, err = f.store.RetireParamKeyChecked(ctx, f.actor, project, key.ID, &key.UpdatedAt, "policy")
	mustAgent(t, err)
	if _, err = f.service.CreateHostFlow(ctx, f.actor, f.project, explicit, platform.RandomID(), "policy"); err == nil {
		t.Fatal("explicit value bypassed retired policy")
	}
	repeated, err = f.service.CreateHostFlow(ctx, f.actor, f.project, in, firstKey, "policy")
	mustAgent(t, err)
	if repeated.ID != first.ID {
		t.Fatal("policy retirement rewrote prior accepted execution")
	}
}
