// keel-agent performs identity, liveness and explicitly enabled read-only tasks.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentclient"
	"github.com/enzoOvOyu/keel/internal/agentproto"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var version = "development"

func main() {
	if run(os.Args[1:]) != nil {
		fmt.Fprintln(os.Stderr, "keel-agent: identity/task transport unavailable; inspect durable operator records")
		os.Exit(1)
	}
}
func run(args []string) error {
	if len(args) == 1 && args[0] == "version" {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"version": version, "capabilities": []string{"enrollment", "mtls-heartbeat", "readonly-tasks", "mtls-renewal"}})
	}
	if len(args) == 0 || (args[0] != "enroll" && args[0] != "run" && args[0] != "renew") {
		return agentclient.ErrIdentity
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	path := f.String("config", "", "absolute protected Agent config path")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 {
		return agentclient.ErrIdentity
	}
	config, e := agentclient.Load(*path)
	if e != nil {
		return e
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if args[0] == "enroll" {
		identity, e := agentclient.Enroll(ctx, config, version)
		if e != nil {
			return e
		}
		return json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "enrolled", "credential_id": identity.CredentialID})
	}
	delay := time.Second
	for {
		received := false
		if config.RenewalEnabled || args[0] == "renew" {
			var renewed bool
			renewed, e = agentclient.RenewIfDue(ctx, config, version)
			if args[0] == "renew" {
				if e != nil {
					return e
				}
				return json.NewEncoder(os.Stdout).Encode(map[string]any{"status": "ready", "renewed": renewed})
			}
		}
		if e == nil {
			e = agentclient.Stream(ctx, config, version, func(_ *agentproto.HeartbeatAck) { received = true })
		}
		if received {
			delay = time.Second
		}
		if ctx.Err() != nil {
			return nil
		}
		if status.Code(e) != codes.Unavailable && status.Code(e) != codes.DeadlineExceeded && status.Code(e) != codes.ResourceExhausted && !errors.Is(e, context.DeadlineExceeded) {
			return e
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		e = nil
		if delay < 30*time.Second {
			delay *= 2
		}
	}
}
