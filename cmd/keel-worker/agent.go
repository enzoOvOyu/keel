package main

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentidentity"
	"github.com/enzoOvOyu/keel/internal/agentrunner"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"google.golang.org/grpc"
)

// Worker-owned task streams stay in the same process as the existing execution
// dispatcher. This is a transient transport pool, never a second durable queue.
func startAgentWorker(ctx context.Context, store *platform.Store, config *agentidentity.Config, broker *agentrunner.Broker) (<-chan error, error) {
	if config == nil {
		return nil, nil
	}
	if !config.TasksEnabled || broker == nil {
		return nil, errors.New("Agent worker task configuration rejected")
	}
	server, err := agentidentity.NewWithTasks(store, *config, broker)
	if err != nil {
		return nil, errors.New("Agent worker TLS configuration rejected")
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return nil, errors.New("Agent worker listener unavailable")
	}
	done := make(chan error, 1)
	go func() {
		served := make(chan error, 1)
		go func() { served <- server.Serve(listener) }()
		select {
		case err := <-served:
			if errors.Is(err, grpc.ErrServerStopped) {
				done <- nil
			} else {
				done <- errors.New("Agent worker listener stopped")
			}
		case <-ctx.Done():
			stopped := make(chan struct{})
			go func() { server.GracefulStop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				server.Stop()
				<-stopped
			}
			<-served
			done <- nil
		}
	}()
	return done, nil
}

// Receipt recovery has its own bounded loop so a missing target cannot block
// the common outbox/lease/claim heartbeat. It only queries existing attempts.
func startAgentReceiptRefresh(ctx context.Context, service *execution.Service) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				work, cancel := context.WithTimeout(ctx, 6*time.Second)
				_, _ = service.RefreshAgentReceipts(work, 4)
				cancel()
			}
		}
	}()
	return done
}
