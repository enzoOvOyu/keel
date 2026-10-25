package main

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentidentity"
	"github.com/enzoOvOyu/keel/internal/platform"
	"google.golang.org/grpc"
)

func optionalAgentIdentity(store *platform.Store) (func(context.Context) error, error) {
	config, err := agentidentity.Load(os.Getenv("KEEL_AGENT_IDENTITY_FILE"))
	if err != nil {
		return nil, errors.New("Agent identity listener configuration rejected")
	}
	if config == nil {
		return nil, nil
	}
	server, err := agentidentity.New(store, *config)
	if err != nil {
		return nil, errors.New("Agent identity TLS configuration rejected")
	}
	listener, err := net.Listen("tcp", config.Listen)
	if err != nil {
		return nil, errors.New("Agent identity listener unavailable")
	}
	return func(ctx context.Context) error {
		done := make(chan error, 1)
		go func() { done <- server.Serve(listener) }()
		select {
		case err := <-done:
			if errors.Is(err, grpc.ErrServerStopped) {
				return nil
			}
			return errors.New("Agent identity listener stopped")
		case <-ctx.Done():
			stopped := make(chan struct{})
			go func() { server.GracefulStop(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(3 * time.Second):
				server.Stop()
				<-stopped
			}
			<-done
			return nil
		}
	}, nil
}
