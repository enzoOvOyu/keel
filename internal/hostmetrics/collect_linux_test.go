//go:build linux

package hostmetrics

import (
	"context"
	"testing"
	"time"
)

func TestAgentlessCollectorDoesNotInventAgentProcess(t *testing.T) {
	samples := CollectGroup(context.Background(), "D6", time.Now().UTC(), &CPUCounter{})
	if len(samples) != 1 || samples[0].Quality != "unknown" || samples[0].State != "" {
		t.Fatal("Agentless collector declared a local Agent running", samples)
	}
}
