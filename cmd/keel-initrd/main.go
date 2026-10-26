//go:build linux

// keel-initrd is the /init of a separately approved ephemeral installation
// image. Read-only preflight and independent storage review precede any
// explicitly permitted write. It never exposes an interactive shell or retry.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/enzoOvOyu/keel/internal/provisioninitrd"
	"github.com/enzoOvOyu/keel/internal/provisionwriter"
	"golang.org/x/sys/unix"
)

func main() {
	if handled, code := provisionwriter.RunToolChild(os.Args[1:]); handled {
		os.Exit(code)
	}
	if provisioninitrd.RequireInitramfs() != nil {
		fmt.Fprintln(os.Stderr, "Keel initrd unavailable: requires root PID1 in an approved ephemeral boot image")
		if os.Getpid() == 1 {
			hold()
		}
		os.Exit(2)
	}
	// Owned helper/tool children are collected by their Cmd.Run. Start the
	// global orphan reaper only after the bounded installation phase returns.
	p, err := provisioninitrd.RunPreparation(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "Keel initrd: stopped; inspect /run/keel for the authenticated write outcome; no automatic retry")
	} else {
		fmt.Fprintf(os.Stdout, "Keel initrd: %s; evidence /run/keel\n", p.Status)
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
