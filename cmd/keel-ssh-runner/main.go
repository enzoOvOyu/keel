// keel-ssh-runner is an operator-installed, per-command SSH helper. It has no
// listener, enrollment, arbitrary shell executor, or default credential.
package main

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	state := os.Getenv("KEEL_SSH_RECEIPT_DIRECTORY")
	if state == "" {
		state = "/var/lib/keel-ssh/receipts"
	}
	if (taskrunner.Wrapper{StateDirectory: state, AuthorizedKeysFile: os.Getenv("KEEL_SSH_AUTHORIZED_KEYS_FILE")}).Serve(ctx, os.Stdin, os.Stdout) != nil {
		os.Exit(1)
	}
}
