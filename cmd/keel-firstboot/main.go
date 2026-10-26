//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "--once" {
		fmt.Fprintln(os.Stderr, "usage: keel-firstboot --once")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	result, err := provisionfirstboot.RunInstalled(ctx)
	fmt.Fprintln(os.Stdout, provisionfirstboot.Diagnostic(result))
	if err != nil {
		os.Exit(1)
	}
}
