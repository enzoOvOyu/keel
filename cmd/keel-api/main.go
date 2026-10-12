package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/platform"
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
	var handler http.Handler = httpapi.New(version, dir)
	if dsn := os.Getenv("KEEL_DATABASE_URL"); dsn != "" {
		config := platform.Config{Environment: os.Getenv("KEEL_ENV"), InsecureDevCookie: os.Getenv("KEEL_INSECURE_DEV_COOKIE") == "true"}
		for _, origin := range strings.Split(os.Getenv("KEEL_PUBLIC_ORIGINS"), ",") {
			if origin = strings.TrimSpace(origin); origin != "" {
				config.PublicOrigins = append(config.PublicOrigins, origin)
			}
		}
		if err := config.Validate(); err != nil {
			slog.Error("authentication configuration rejected", "error", err)
			os.Exit(1)
		}
		connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		store, err := platform.Open(connectCtx, dsn)
		cancel()
		if err != nil {
			slog.Error("database unavailable; refusing to start")
			os.Exit(1)
		}
		defer store.Close()
		checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = store.CheckMigrations(checkCtx)
		cancel()
		if err != nil {
			slog.Error("database schema rejected", "error", err)
			os.Exit(1)
		}
		handler = httpapi.NewWithStore(version, dir, store, config)
	}
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("listen", "error", err)
		os.Exit(1)
	}
	slog.Info("keel API listening", "address", listener.Addr().String(), "version", version)
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
