package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/httpapi"
)

var version = "development"

func main() {
	addr := os.Getenv("KEEL_LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:3000"
	}
	dir := os.Getenv("KEEL_STATIC_DIR")
	if dir == "" {
		dir = "web/dist"
	}
	srv := &http.Server{Addr: addr, Handler: httpapi.New(version, dir), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("listen", "error", err)
		os.Exit(1)
	}
	slog.Info("keel foundation listening", "address", listener.Addr().String(), "version", version)
	if err := serve(ctx, srv, listener); err != nil {
		slog.Error("server", "error", err)
		os.Exit(1)
	}
}

// serve keeps the main goroutine alive until active requests have drained.
// Server.Serve returns as soon as Shutdown closes the listener, before that drain.
func serve(ctx context.Context, srv *http.Server, listener net.Listener) error {
	serveResult := make(chan error, 1)
	go func() { serveResult <- srv.Serve(listener) }()
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			// A timed-out drain must not leave connections running past process exit.
			_ = srv.Close()
			return err
		}
		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
