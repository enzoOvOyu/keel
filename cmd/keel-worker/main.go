// keel-worker executes only the safe, explicitly simulated adapters.
package main

import (
	"context"
	"flag"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	health := flag.Bool("healthcheck", false, "verify this worker's persistent simulation heartbeat")
	flag.Parse()
	id := os.Getenv("KEEL_WORKER_ID")
	if id == "" {
		id, _ = os.Hostname()
	}
	if id == "" || len(id) > 120 {
		fail("invalid worker identity")
	}
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		fail("KEEL_DATABASE_URL is required")
	}
	boot, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	store, e := platform.Open(boot, dsn)
	cancel()
	if e != nil {
		fail("worker database unavailable")
	}
	defer store.Close()
	service := execution.New(store.DB)
	if *health {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if service.WorkerHealthy(ctx, id) != nil {
			fail("simulation worker heartbeat is stale")
		}
		return
	}
	boot, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	e = store.CheckMigrations(boot)
	cancel()
	if e != nil {
		fail("database migration required")
	}
	concurrency := 8
	if text := os.Getenv("KEEL_WORKER_CONCURRENCY"); text != "" {
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 64 {
			fail("KEEL_WORKER_CONCURRENCY must be 1..64")
		}
		concurrency = n
	}
	lease := 5 * time.Second
	if text := os.Getenv("KEEL_WORKER_LEASE"); text != "" {
		d, err := time.ParseDuration(text)
		if err != nil || d < time.Second || d > time.Minute {
			fail("KEEL_WORKER_LEASE must be 1s..1m")
		}
		lease = d
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var jobs sync.WaitGroup
	var active atomic.Int64
	lastBeat := time.Time{}
	slog.Info("keel simulation worker started", "worker", id, "mode", "sim", "concurrency", concurrency)
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			if _, e = service.PumpOutbox(ctx, id, 100); e != nil {
				slog.Error("worker outbox operation failed")
				continue
			}
			if _, e = service.ReapExpired(ctx); e != nil {
				slog.Error("worker reconciliation scan failed")
				continue
			}
			cycleOK := true
			for int(active.Load()) < concurrency {
				claim, err := service.Claim(ctx, id, lease)
				if err != nil {
					slog.Error("worker claim failed")
					cycleOK = false
					break
				}
				if claim == nil {
					break
				}
				active.Add(1)
				jobs.Add(1)
				go func(c *execution.Claim) {
					defer jobs.Done()
					defer active.Add(-1)
					if err := service.RunClaim(ctx, c); err != nil {
						slog.Warn("simulator receipt requires reconciliation", "attempt", c.Attempt.ID)
					}
				}(claim)
			}
            // Health means queue/reconciliation SQL succeeded, not merely that
            // the process can touch its heartbeat table.
            if cycleOK && time.Since(lastBeat)>=time.Second {
                if service.WorkerHeartbeat(ctx,id,int(active.Load()))==nil {lastBeat=time.Now()}
            }
		}
	}
	// Stop new claims immediately, continue lease heartbeats until each finite
	// simulator confirms its stop/safe endpoint. Container stop grace must be 70s.
	jobs.Wait()
	slog.Info("keel simulation worker drained", "worker", id)
}
func fail(message string) { slog.Error(message); os.Exit(1) }
