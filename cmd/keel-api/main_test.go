package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestServeDrainsInflightRequestBeforeReturning(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	shutdownStarted := make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "request completed")
	})}
	srv.RegisterOnShutdown(func() { close(shutdownStarted) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		releaseRequest()
		_ = srv.Close()
		_ = listener.Close()
	})
	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, listener) }()
	type responseResult struct {
		body   string
		status int
		err    error
	}
	response := make(chan responseResult, 1)
	client := &http.Client{Timeout: 5 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	go func() {
		res, err := client.Get("http://" + listener.Addr().String())
		if err != nil {
			response <- responseResult{err: err}
			return
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		response <- responseResult{body: string(body), status: res.StatusCode, err: err}
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached handler")
	}
	cancel()
	select {
	case <-shutdownStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not start")
	}
	select {
	case err := <-done:
		t.Fatalf("serve returned before active request drained: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	releaseRequest()
	select {
	case res := <-response:
		if res.err != nil || res.status != http.StatusOK || res.body != "request completed" {
			t.Fatalf("active request interrupted: %+v", res)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active request did not finish")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after request drained")
	}
}
