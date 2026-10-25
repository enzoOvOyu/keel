package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/webhook"
)

// Each bounded sink has its own loop. Notification I/O cannot stall the
// execution scheduler, heartbeats, or another receiver's deliveries.
func startWebhookWorker(ctx context.Context, store *platform.Store, config *webhook.Config) (<-chan struct{}, error) {
	if config == nil {
		return nil, nil
	}
	dispatcher := &platform.WebhookDispatcher{DB: store.DB, Config: config}
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := dispatcher.Bind(setup)
	cancel()
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	var sinks sync.WaitGroup
	for i := range config.Sinks {
		sinks.Add(1)
		go func(sink *webhook.Sink) {
			defer sinks.Done()
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			var lastReport time.Time
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
				operation, cancel := context.WithTimeout(ctx, 12*time.Second)
				_, err := dispatcher.Enqueue(operation, sink)
				if err == nil {
					_, err = dispatcher.DeliverOne(operation, sink)
				}
				cancel()
				if err != nil && ctx.Err() == nil && time.Since(lastReport) >= 30*time.Second {
					slog.Warn("webhook delivery requires recovery", "sink", sink.ID)
					lastReport = time.Now()
				}
			}
		}(&config.Sinks[i])
	}
	go func() { sinks.Wait(); close(done) }()
	return done, nil
}
