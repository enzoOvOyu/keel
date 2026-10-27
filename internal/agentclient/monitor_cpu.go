package agentclient

import "github.com/enzoOvOyu/keel/internal/hostmetrics"

// Preserve the internal collector test seam while sharing counter semantics
// with bounded Agentless snapshots.
type monitorCPUCounter struct{ counter hostmetrics.CPUCounter }

func (c *monitorCPUCounter) observe(raw []byte) (float64, bool) { return c.counter.Observe(raw) }
