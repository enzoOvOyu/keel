// keel-ssh-runner is an operator-installed, per-command SSH helper. It has no
// listener, enrollment or default credential. Approved scripts require an explicit opt-in.
package main

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer stop()
	state := os.Getenv("KEEL_SSH_RECEIPT_DIRECTORY")
	if state == "" {
		state = "/var/lib/keel-ssh/receipts"
	}
	scripts := os.Getenv("KEEL_SSH_SCRIPTS_ENABLED") == "true"
	if scripts && taskrunner.PrepareScriptProcess() != nil {
		os.Exit(1)
	}
	if (taskrunner.Wrapper{ScriptMediaCacheEnabled: os.Getenv("KEEL_TASK_MEDIA_CACHE_ENABLED") == "true", CacheInventoryEnabled: os.Getenv("KEEL_TASK_CACHE_INVENTORY_ENABLED") == "true", ScriptProcessIsolation: scripts, StateDirectory: state, AuthorizedKeysFile: os.Getenv("KEEL_SSH_AUTHORIZED_KEYS_FILE")}).Serve(ctx, os.Stdin, os.Stdout) != nil {
		os.Exit(1)
	}
}
