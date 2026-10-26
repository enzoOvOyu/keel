package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/ldapauth"
	"github.com/enzoOvOyu/keel/internal/mediaartifact"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"github.com/enzoOvOyu/keel/internal/mediasource"
	"github.com/enzoOvOyu/keel/internal/oidcauth"
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
	var store *platform.Store
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
		opened, err := platform.Open(connectCtx, dsn)
		cancel()
		if err != nil {
			slog.Error("database unavailable; refusing to start")
			os.Exit(1)
		}
		store = opened
		defer store.Close()
		store.AgentTasksEnabled = os.Getenv("KEEL_AGENT_TASKS_ENABLED") == "true"
		store.ProvisionPrepareEnabled = os.Getenv("KEEL_PROVISION_PREPARE_ENABLED") == "true"
		if err = store.ConfigureProvisionInstall(os.Getenv("KEEL_PROVISION_INSTALL_ENABLED") == "true", os.Getenv("KEEL_PROVISION_IMAGE_PUBLIC_KEY_FILE")); err != nil {
			slog.Error("provision installer public trust configuration rejected")
			os.Exit(1)
		}
		store.LDAP, err = ldapauth.Load(os.Getenv("KEEL_LDAP_CONFIG_FILE"))
		if err != nil {
			slog.Error("directory authentication configuration rejected")
			os.Exit(1)
		}
		oidcCtx, oidcCancel := context.WithTimeout(context.Background(), 30*time.Second)
		store.OIDC, err = oidcauth.Load(oidcCtx, os.Getenv("KEEL_OIDC_PROVIDERS_FILE"), config.PublicOrigins, config.Environment == "development" && config.InsecureDevCookie)
		oidcCancel()
		if err != nil {
			slog.Error("organization authentication configuration rejected")
			os.Exit(1)
		}
		checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = store.CheckMigrations(checkCtx)
		cancel()
		if err != nil {
			slog.Error("database schema rejected", "error", err)
			os.Exit(1)
		}
		store.SSHScriptsEnabled = os.Getenv("KEEL_SSH_SCRIPTS_ENABLED") == "true"
		store.AgentScriptsEnabled = os.Getenv("KEEL_AGENT_SCRIPTS_ENABLED") == "true"
		err = store.ConfigureSSHRunner(os.Getenv("KEEL_SSH_RUNNER_FILE"), os.Getenv("KEEL_CREDENTIAL_MASTER_KEY_FILE"))
		if err != nil {
			slog.Error("SSH runner configuration rejected")
			os.Exit(1)
		}
		store.FabricProbes, err = fabricboot.LoadProbeSuite(os.Getenv("KEEL_FABRIC_PROBES_FILE"))
		if err != nil {
			slog.Error("Fabric probe configuration rejected", "error", err)
			os.Exit(1)
		}
		objects, storageErr := mediaartifact.Load()
		if storageErr != nil {
			slog.Error("media storage configuration rejected")
			os.Exit(1)
		}
		if objects != nil {
			store.MediaObjects = objects
		}
		if store.ProvisionInstallEnabled && store.MediaObjects == nil {
			slog.Error("provision installation requires configured media storage")
			os.Exit(1)
		}
		store.MediaManifestSigner, err = mediamanifest.LoadSigner(os.Getenv("KEEL_MEDIA_SIGNING_KEY_FILE"))
		if err != nil {
			slog.Error("media manifest signing configuration rejected")
			os.Exit(1)
		}
		store.MediaSources, err = mediasource.Load()
		if err != nil {
			slog.Error("media source policy configuration rejected")
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
	sztpSrv, sztpListener, err := optionalSZTPListener(store)
	if err != nil {
		_ = listener.Close()
		slog.Error("SZTP listener rejected", "error", err)
		os.Exit(1)
	}
	bootSrv, bootListener, err := optionalZTP239Listener(store)
	if err != nil {
		_ = listener.Close()
		if sztpListener != nil {
			_ = sztpListener.Close()
		}
		slog.Error("ZTP 239 listener rejected", "error", err)
		os.Exit(1)
	}
	if store != nil {
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					workCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
					_, reconcileErr := store.ReconcileZTPDeviceSessions(workCtx, 16)
					cancel()
					if reconcileErr != nil && ctx.Err() == nil {
						slog.Error("ZTP management reconciliation", "error", reconcileErr)
					}
				}
			}
		}()
	}
	if store != nil {
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					workCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
					_, retryErr := store.RetryDueZTPSimSessions(workCtx, 32)
					cancel()
					if retryErr != nil && ctx.Err() == nil {
						slog.Error("ZTP simulation retry", "error", retryErr)
					}
				}
			}
		}()
	}
	servers := []struct {
		server   *http.Server
		listener net.Listener
	}{{srv, listener}}
	if sztpSrv != nil {
		slog.Info("keel SZTP listening", "address", sztpListener.Addr().String())
		servers = append(servers, struct {
			server   *http.Server
			listener net.Listener
		}{sztpSrv, sztpListener})
	}
	if bootSrv != nil {
		slog.Info("keel ZTP 239 listening", "address", bootListener.Addr().String())
		servers = append(servers, struct {
			server   *http.Server
			listener net.Listener
		}{bootSrv, bootListener})
	}
	agentServe, agentErr := optionalAgentIdentity(store)
	if agentErr != nil {
		for _, entry := range servers {
			_ = entry.listener.Close()
		}
		slog.Error("Agent identity listener rejected")
		os.Exit(1)
	}
	serverCount := len(servers)
	if agentServe != nil {
		serverCount++
	}
	results := make(chan error, serverCount)
	if agentServe != nil {
		go func() { results <- agentServe(ctx) }()
	}
	for _, entry := range servers {
		go func(server *http.Server, listener net.Listener) { results <- serve(ctx, server, listener) }(entry.server, entry.listener)
	}
	first := <-results
	stop()
	for range serverCount - 1 {
		if next := <-results; next != nil && first == nil {
			first = next
		}
	}
	if first != nil {
		slog.Error("server", "error", first)
		os.Exit(1)
	}
}

func optionalZTP239Listener(store *platform.Store) (*http.Server, net.Listener, error) {
	addr := os.Getenv("KEEL_ZTP239_LISTEN_ADDR")
	certFile := os.Getenv("KEEL_ZTP239_TLS_CERT_FILE")
	keyFile := os.Getenv("KEEL_ZTP239_TLS_KEY_FILE")
	if addr == "" && certFile == "" && keyFile == "" {
		return nil, nil, nil
	}
	if addr == "" || certFile == "" || keyFile == "" || store == nil {
		return nil, nil, errors.New("ZTP 239 requires database, listen address and TLS certificate/key")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	config := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	srv := &http.Server{Addr: addr, Handler: httpapi.NewZTP239Handler(store), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
	return srv, tls.NewListener(listener, config), nil
}

func optionalSZTPListener(store *platform.Store) (*http.Server, net.Listener, error) {
	addr := os.Getenv("KEEL_SZTP_LISTEN_ADDR")
	certFile := os.Getenv("KEEL_SZTP_TLS_CERT_FILE")
	keyFile := os.Getenv("KEEL_SZTP_TLS_KEY_FILE")
	caFile := os.Getenv("KEEL_SZTP_IDEVID_CA_FILE")
	if addr == "" && certFile == "" && keyFile == "" && caFile == "" {
		return nil, nil, nil
	}
	if addr == "" || certFile == "" || keyFile == "" || caFile == "" || store == nil {
		return nil, nil, errors.New("SZTP requires database, listen address, TLS certificate/key and manufacturer CA file")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, nil, err
	}
	raw, err := os.ReadFile(caFile)
	if err != nil {
		return nil, nil, err
	}
	roots, err := fabricboot.ParseIDevIDRoots(raw)
	if err != nil {
		return nil, nil, err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, MinVersion: tls.VersionTLS12}
	handler := httpapi.NewSZTPHandler(store, roots)
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 16}
	return srv, tls.NewListener(listener, tlsConfig), nil
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
