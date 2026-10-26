//go:build linux

// keel-provision is an explicitly enabled isolated server boot service.
// It never assigns DHCP leases, starts a disk writer or establishes SSH trust.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/mediaartifact"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

func main() {
	if err := run(); err != nil {
		slog.Error("isolated provision service stopped; no installation success implied")
		os.Exit(1)
	}
}
func run() error {
	if os.Getenv("KEEL_PROVISION_SERVICE_ENABLED") != "true" || os.Getenv("KEEL_PROVISION_INSTALL_ENABLED") != "true" {
		return errors.New("isolated provisioning service disabled")
	}
	project, scopeID := os.Getenv("KEEL_PROVISION_PROJECT_ID"), os.Getenv("KEEL_PROVISION_SCOPE_ID")
	if project == "" || scopeID == "" || os.Getenv("KEEL_DATABASE_URL") == "" {
		return errors.New("explicit project, approved scope and database are required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	startup, done := context.WithTimeout(ctx, 15*time.Second)
	store, err := platform.Open(startup, os.Getenv("KEEL_DATABASE_URL"))
	if err != nil {
		done()
		return err
	}
	defer store.Close()
	if err = store.CheckMigrations(startup); err != nil {
		done()
		return err
	}
	if err = store.ConfigureProvisionInstall(true, os.Getenv("KEEL_PROVISION_IMAGE_PUBLIC_KEY_FILE")); err != nil {
		done()
		return err
	}
	store.MediaObjects, err = mediaartifact.Load()
	if err != nil || store.MediaObjects == nil {
		done()
		return errors.New("configured media storage required")
	}
	load := func(ctx context.Context) (provisionboot.Scope, error) {
		tx, e := store.DB.BeginTx(ctx, nil)
		if e != nil {
			return provisionboot.Scope{}, e
		}
		defer tx.Rollback()
		x, e := platform.LoadApprovedProvisionScopeTx(ctx, tx, project, scopeID)
		if e != nil {
			return provisionboot.Scope{}, e
		}
		return x.Scope, tx.Commit()
	}
	scope, err := load(startup)
	done()
	if err != nil {
		return err
	}
	cert, err := agentpki.ReadFile(os.Getenv("KEEL_PROVISION_TLS_CERT_FILE"), 64<<10, false)
	if err != nil {
		return err
	}
	key, err := agentpki.ReadFile(os.Getenv("KEEL_PROVISION_TLS_KEY_FILE"), 64<<10, true)
	if err != nil {
		return err
	}
	defer clear(key)
	certificate, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return errors.New("invalid provision HTTPS key pair")
	}
	authority := &platform.ProvisionBootAuthority{Store: store}
	work, stop := context.WithCancel(ctx)
	defer stop()
	results := make(chan error, 4)
	var workers sync.WaitGroup
	start := func(fn func() error) { workers.Add(1); go func() { defer workers.Done(); results <- fn() }() }
	start(func() error {
		return provisionboot.ServeHTTPS(work, provisionboot.HTTPSConfig{Enabled: true, Scope: scope, Certificate: certificate}, authority, authority)
	})
	start(func() error {
		return provisionboot.ServeProxy(work, provisionboot.ProxyConfig{Enabled: true, Scope: scope}, authority)
	})
	start(func() error {
		return provisionboot.ServeTFTP(work, provisionboot.TFTPConfig{Enabled: true, Scope: scope}, authority)
	})
	start(func() error {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return work.Err()
			case <-ticker.C:
				check, finish := context.WithTimeout(work, 3*time.Second)
				_, e := load(check)
				finish()
				if e != nil {
					return e
				}
			}
		}
	})
	err = <-results
	stop()
	workers.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if err == nil {
		return errors.New("provision transport stopped unexpectedly")
	}
	return err
}
