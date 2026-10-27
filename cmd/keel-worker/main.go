// keel-worker executes simulation and operator-enabled Kea control adapters.
package main

import (
	"context"
	"flag"
	"github.com/enzoOvOyu/keel/internal/agentidentity"
	"github.com/enzoOvOyu/keel/internal/agentrunner"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/executionqueue"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/mediaartifact"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/webhook"
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
	health := flag.Bool("healthcheck", false, "verify this worker's persistent execution heartbeat")
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
	store.SSHScriptsEnabled = os.Getenv("KEEL_SSH_SCRIPTS_ENABLED") == "true"
	store.AgentScriptsEnabled = os.Getenv("KEEL_AGENT_SCRIPTS_ENABLED") == "true"
	store.AgentNetworkInventoryEnabled = os.Getenv("KEEL_AGENT_NETWORK_INVENTORY_ENABLED") == "true"
	store.TaskCacheInventoryEnabled = os.Getenv("KEEL_TASK_CACHE_INVENTORY_ENABLED") == "true"
	store.ArtifactTransfersEnabled = os.Getenv("KEEL_TASK_ARTIFACT_TRANSFERS_ENABLED") == "true"
	e = store.ConfigureSSHRunner(os.Getenv("KEEL_SSH_RUNNER_FILE"), os.Getenv("KEEL_CREDENTIAL_MASTER_KEY_FILE"))
	if e != nil {
		slog.Error("SSH runner configuration rejected")
		os.Exit(1)
	}
	store.FabricProbes, e = fabricboot.LoadProbeSuite(os.Getenv("KEEL_FABRIC_PROBES_FILE"))
	if e != nil {
		fail("fabric probe configuration rejected")
	}
	store.FabricIssueVerificationEnabled = os.Getenv("KEEL_FABRIC_ISSUE_VERIFY_ENABLED") == "true"
	store.BMCInventoryEnabled = os.Getenv("KEEL_BMC_INVENTORY_ENABLED") == "true"
	if err := store.ConfigureBMCInventory(os.Getenv("KEEL_BMC_INVENTORY_FILE"), os.Getenv("KEEL_DEVICE_CREDENTIAL_MASTER_KEY_FILE")); err != nil {
		slog.Error("BMC inventory configuration rejected")
		os.Exit(1)
	}

	store.ProvisionPrepareEnabled = os.Getenv("KEEL_PROVISION_PREPARE_ENABLED") == "true"
	if e = store.ConfigureProvisionInstall(os.Getenv("KEEL_PROVISION_INSTALL_ENABLED") == "true", os.Getenv("KEEL_PROVISION_IMAGE_PUBLIC_KEY_FILE")); e != nil {
		fail("provision installer public trust configuration rejected")
	}
	if store.BMCInventoryEnabled || store.ProvisionPrepareEnabled || store.ProvisionInstallEnabled || store.SSHScriptsEnabled || store.AgentScriptsEnabled || store.ArtifactTransfersEnabled {
		store.MediaObjects, e = mediaartifact.Load()
		if e != nil || store.MediaObjects == nil {
			fail("Provision preparation requires configured media storage")
		}
	}
	service := execution.New(store.DB)
	queue, queueErr := executionqueue.Load(os.Getenv("KEEL_EXECUTION_QUEUE_FILE"), os.Getenv("KEEL_ENV") == "development")
	if queueErr != nil {
		fail("execution queue configuration rejected")
	}
	if queue != nil {
		service.Queue = queue
		defer queue.Close()
	}
	service.MediaStore = store
	if store.BMCInventoryEnabled && store.BMCInventory != nil {
		service.BMCStore = store
	}
	if store.ProvisionPrepareEnabled || store.ProvisionInstallEnabled {
		service.ProvisionStore = store
	}

	if store.SSHRunner != nil {
		service.SSHRunner = store.SSHRunner
	}
	if _, _, e = store.FabricProbes.DeploymentControl(); e == nil {
		service.KeaStore = store
	}
	if _, e = store.FabricProbes.NetworkClient(); e == nil {
		service.NetworkStore = store
	}
	if store.FabricIssueVerificationEnabled && store.FabricProbes.CollectorConfigured() {
		service.IssueStore = store
	}
	agentConfig, e := agentidentity.Load(os.Getenv("KEEL_AGENT_WORKER_FILE"))
	if e != nil || (agentConfig != nil && !agentConfig.TasksEnabled) {
		fail("Agent worker configuration rejected")
	}
	var agentBroker *agentrunner.Broker
	if agentConfig != nil {
		agentBroker = agentrunner.New(store)
		service.AgentRunner = agentBroker
	}
	if *health {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if service.WorkerHealthy(ctx, id) != nil {
			fail("execution worker heartbeat is stale")
		}
		return
	}
	boot, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	e = store.CheckMigrations(boot)
	cancel()
	if e != nil {
		fail("database migration required")
	}
	webhooks, e := webhook.Load(os.Getenv("KEEL_WEBHOOK_SINKS_FILE"), os.Getenv("KEEL_ENV") == "development")
	if e != nil {
		fail("webhook sink configuration rejected")
	}
	if webhooks != nil {
		defer webhooks.Close()
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
	webhookDone, e := startWebhookWorker(ctx, store, webhooks)
	if e != nil {
		fail("webhook sink policy rejected")
	}
	agentDone, e := startAgentWorker(ctx, store, agentConfig, agentBroker)
	if e != nil {
		fail("Agent worker listener configuration rejected")
	}
	var refreshDone <-chan struct{}
	if service.AgentRunner != nil || service.SSHRunner != nil {
		refreshDone = startHostReceiptRefresh(ctx, service)
	}
	listenerFailed := false
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var jobs sync.WaitGroup
	var active atomic.Int64
	lastBeat := time.Time{}
	queueMode := "postgresql-only"
	if service.Queue != nil {
		queueMode = "redis-streams"
	}
	slog.Info("keel execution worker started", "worker", id, "queue_mode", queueMode, "kea_enabled", service.KeaStore != nil, "agent_enabled", service.AgentRunner != nil, "concurrency", concurrency)
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case err := <-agentDone:
			agentDone = nil
			if err != nil {
				slog.Error("Agent task listener failed")
				listenerFailed = true
			}
			stop()
			break loop
		case <-ticker.C:
			queueCtx, queueCancel := context.WithTimeout(ctx, 3*time.Second)
			_, queueErr = service.PumpQueue(queueCtx, id, 100)
			queueCancel()
			queueOK := queueErr == nil
			if !queueOK {
				slog.Error("worker outbox operation failed")
			}
			if _, e = service.ReapExpired(ctx); e != nil {
				slog.Error("worker reconciliation scan failed")
				continue
			}
			cycleOK := queueOK
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
						slog.Warn("runner receipt requires reconciliation", "attempt", c.Attempt.ID)
					}
				}(claim)
			}
			// Health means queue/reconciliation SQL succeeded, not merely that
			// the process can touch its heartbeat table.
			if cycleOK && time.Since(lastBeat) >= time.Second {
				if service.WorkerHeartbeat(ctx, id, int(active.Load())) == nil {
					lastBeat = time.Now()
				}
			}
		}
	}
	// Stop new claims immediately, continue lease heartbeats until each finite
	// simulator confirms its stop/safe endpoint. Container stop grace must be 70s.
	jobs.Wait()
	if agentDone != nil {
		<-agentDone
	}
	if refreshDone != nil {
		<-refreshDone
	}
	if webhookDone != nil {
		<-webhookDone
	}
	if listenerFailed {
		fail("Agent task listener stopped unexpectedly")
	}
	slog.Info("keel execution worker drained", "worker", id)
}
func fail(message string) { slog.Error(message); os.Exit(1) }
