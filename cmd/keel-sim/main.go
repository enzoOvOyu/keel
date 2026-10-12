// keel-sim emits reproducible, public simulation input manifests. It neither
// initializes real assets nor contacts arbitrary network/hardware addresses.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/execution"
	"os"
)

func main() {
	seed := flag.Int64("seed", 1, "reproducible seed")
	count := flag.Int("targets", 2000, "logical targets (1..2000)")
	prefix := flag.String("prefix", "sim", "registration prefix")
	scenario := flag.String("scenario", "success", "success|failed|drop_receipt")
	flag.Parse()
	if *count < 1 || *count > 2000 {
		fmt.Fprintln(os.Stderr, "targets must be 1..2000")
		os.Exit(1)
	}
	d := execution.Definition{APIVersion: "keel.sim/v1", Inputs: map[string]execution.Input{}, Tasks: []execution.Task{{ID: "inspect", Type: "sim.read", Retry: 0, Idempotent: true, TimeoutMS: 1000, Resources: []string{"machine"}, Sim: execution.SimSpec{Outcome: *scenario, DurationMS: 30, Seed: *seed}}}}
	if e := d.Validate(); e != nil {
		fmt.Fprintln(os.Stderr, "unsupported simulation scenario")
		os.Exit(1)
	}
	out := map[string]any{"mode": "sim", "hardware_evidence": false, "register_targets": execution.RegisterInput{Prefix: *prefix, Count: *count, Seed: *seed}, "publish_flow": execution.PublishInput{Name: "Simulation inspection", Definition: &d}, "instructions": "Register targets and publish this flow through the authorized project API; create an execution with the returned immutable IDs. No hardware operations are available."}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if encoder.Encode(out) != nil {
		os.Exit(1)
	}
}
