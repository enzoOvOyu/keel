//go:build linux

// keel-initrd is the /init of a separately approved ephemeral installation
// image. This first phase retains read-only evidence and waits for storage
// review. It deliberately has no shell, disk writer or automatic reboot path.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/enzoOvOyu/keel/internal/provisioninitrd"
	"golang.org/x/sys/unix"
)

func main() {
	if provisioninitrd.RequireInitramfs() != nil {
		fmt.Fprintln(os.Stderr, "Keel initrd unavailable: requires root PID1 in an approved ephemeral boot image")
		if os.Getpid() == 1 {
			hold()
		}
		os.Exit(2)
	}
	// The boot phase owns only the fixed read-only helper child. Do not start a
	// global Wait4 reaper until Cmd.Run has collected that child's exit status.
	p, err := provisioninitrd.RunPreparation(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Keel initrd: BLOCKED; installation not performed")
	} else {
		fmt.Fprintf(os.Stdout, "Keel initrd: %s; installation not performed; evidence /run/keel\n", p.Status)
	}
	hold()
}

// PID1 remains alive, reaps adopted children, and never turns a signal into an
// implicit retry, reboot, shell or installation. Recovery requires a new boot.
func hold() {
	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGCHLD, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	for {
		for {
			pid, err := unix.Wait4(-1, nil, unix.WNOHANG, nil)
			if err == unix.EINTR {
				continue
			}
			if err != nil || pid <= 0 {
				break
			}
		}
		<-signals
	}
}
