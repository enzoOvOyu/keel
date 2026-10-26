//go:build linux

// keel-preflight runs inside an approved installation environment. It only emits
// local read-only evidence; it does not contact a controller or start an install.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

func main() {
	specFile := flag.String("spec-file", "", "frozen controller-issued preflight specification JSON")
	flag.Parse()
	if *specFile == "" || flag.NArg() != 0 {
		fail("an exact frozen --spec-file is required", 2)
	}
	file, err := os.Open(*specFile)
	if err != nil {
		fail("specification unavailable", 2)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		fail("specification must be a regular bounded file", 2)
	}
	raw, err := io.ReadAll(io.LimitReader(file, (64<<10)+1))
	file.Close()
	if err != nil {
		fail("specification unreadable", 2)
	}
	spec, err := provisionpreflight.DecodeSpec(raw)
	if err != nil || spec.Validate(time.Now()) != nil {
		fail("invalid or expired frozen specification", 2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	observation := provisionpreflight.NewCollector().Collect(ctx)
	report, err := provisionpreflight.Evaluate(spec, observation, time.Now())
	if err != nil {
		fail("specification expired during read-only collection", 2)
	}
	if err = json.NewEncoder(os.Stdout).Encode(report); err != nil {
		fail("evidence output failed", 2)
	}
	if report.Status != "PASS" {
		os.Exit(1)
	}
}
func fail(message string, code int) { fmt.Fprintln(os.Stderr, message); os.Exit(code) }
