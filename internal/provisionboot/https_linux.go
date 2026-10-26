//go:build linux

package provisionboot

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type HTTPSConfig struct {
	Enabled       bool
	Scope         Scope
	Certificate   tls.Certificate
	MaxConcurrent int
}

func (c HTTPSConfig) validate() (string, error) {
	if !c.Enabled || c.Scope.Validate() != nil || c.Scope.Development || c.MaxConcurrent < 0 || c.MaxConcurrent > 64 || len(c.Certificate.Certificate) == 0 || c.Certificate.PrivateKey == nil {
		return "", ErrDenied
	}
	origin, err := url.Parse(c.Scope.Origin)
	if err != nil || origin.Scheme != "https" {
		return "", ErrDenied
	}
	port := origin.Port()
	if port == "" {
		port = "443"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return "", ErrDenied
	}
	leaf, err := x509.ParseCertificate(c.Certificate.Certificate[0])
	if err != nil || leaf.VerifyHostname(origin.Hostname()) != nil || time.Now().Before(leaf.NotBefore) || !leaf.NotAfter.After(time.Now()) {
		return "", ErrDenied
	}
	return net.JoinHostPort(c.Scope.Server.String(), port), nil
}

// ServeHTTPS is explicit and isolated from the API/worker. It binds only the
// approved server address AND interface, never a wildcard or proxy listener.
// Both public files and signed callbacks use the same current DB authority.
func ServeHTTPS(ctx context.Context, config HTTPSConfig, files FileAuthority, callbacks CallbackAuthority) error {
	address, err := config.validate()
	if err != nil || files == nil || callbacks == nil {
		return ErrDenied
	}
	if _, err = scopeInterface(config.Scope); err != nil {
		return err
	}
	listenerConfig := net.ListenConfig{Control: func(network, address string, raw syscall.RawConn) error {
		var bindErr error
		err := raw.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, config.Scope.Interface)
		})
		if err != nil {
			return err
		}
		return bindErr
	}}
	listener, err := listenerConfig.Listen(ctx, "tcp4", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	gateway := &Gateway{Scope: config.Scope, Authority: files, MaxConcurrent: config.MaxConcurrent}
	callback := &CallbackGateway{Scope: config.Scope, Authority: callbacks, MaxConcurrent: config.MaxConcurrent}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			callback.ServeHTTP(w, r)
		} else {
			gateway.ServeHTTP(w, r)
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{config.Certificate}, NextProtos: []string{"http/1.1"}}}
	stop := context.AfterFunc(ctx, func() { _ = server.Close() })
	defer stop()
	err = server.Serve(tls.NewListener(listener, server.TLSConfig))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
