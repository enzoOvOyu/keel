//go:build linux

package provisionboot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/netip"
	"sync"
	"time"
)

type TFTPConfig struct {
	Enabled       bool
	Scope         Scope
	Port          uint16
	MaxConcurrent int
}

// ServeTFTP provides a bounded read-only bootstrap daemon with a separate
// ephemeral transfer ID/socket for each client. No implicit interface binding,
// filesystem root, write request or arbitrary media path is exposed.
func ServeTFTP(ctx context.Context, config TFTPConfig, authority FileAuthority) error {
	if !config.Enabled || authority == nil || config.Scope.Validate() != nil {
		return ErrDenied
	}
	port := config.Port
	if port == 0 {
		port = 69
	}
	if (!config.Scope.Development && port != 69) || (config.Scope.Development && port != 69 && port < 1024) {
		return ErrDenied
	}
	limit := config.MaxConcurrent
	if limit == 0 {
		limit = 32
	}
	if limit < 1 || limit > 64 {
		return ErrDenied
	}
	index, err := scopeInterface(config.Scope)
	if err != nil {
		return err
	}
	udp, wire, err := listenScoped(ctx, config.Scope, port, config.Scope.Server.String())
	if err != nil {
		return err
	}
	defer udp.Close()
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(run, func() { udp.Close() })
	defer stop()
	slots := make(chan struct{}, limit)
	active := map[string]bool{}
	var mu sync.Mutex
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	buffer := make([]byte, 513)
	for {
		n, control, source, e := wire.ReadFrom(buffer)
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return e
		}
		if n > 512 || control == nil || control.IfIndex != index {
			continue
		}
		destination, ok := netip.AddrFromSlice(control.Dst.To4())
		if !ok || destination != config.Scope.Server {
			continue
		}
		peer, ok := source.(*net.UDPAddr)
		if !ok || peer.Port == 0 {
			continue
		}
		ip, ok := netip.AddrFromSlice(peer.IP.To4())
		if !ok || !config.Scope.CheckPeer(ip) {
			continue
		}
		packet := append([]byte{}, buffer[:n]...)
		if _, _, e = ParseTFTPRequest(packet); e != nil {
			continue
		}
		sum := sha256.Sum256(packet)
		key := peer.String() + ":" + hex.EncodeToString(sum[:])
		mu.Lock()
		if active[key] {
			mu.Unlock()
			continue
		}
		select {
		case slots <- struct{}{}:
			active[key] = true
		default:
			mu.Unlock()
			continue
		}
		mu.Unlock()
		workers.Add(1)
		go func(peer *net.UDPAddr, ip netip.Addr, packet []byte, key string) {
			defer workers.Done()
			defer func() { <-slots; mu.Lock(); delete(active, key); mu.Unlock() }()
			serveTFTPTransfer(run, config.Scope, authority, peer, ip, packet, index)
		}(peer, ip, packet, key)
	}
}
func serveTFTPTransfer(parent context.Context, scope Scope, authority FileAuthority, peer *net.UDPAddr, ip netip.Addr, request []byte, index int) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	transfer, err := OpenTFTP(ctx, scope, authority, ip, request)
	if err != nil {
		return
	}
	defer transfer.Close()
	udp, wire, err := listenScoped(ctx, scope, 0, scope.Server.String())
	if err != nil {
		return
	}
	defer udp.Close()
	stop := context.AfterFunc(ctx, func() { udp.Close() })
	defer stop()
	packet, err := transfer.Next(nil)
	if err != nil {
		return
	}
	rto := time.Second
	buffer := make([]byte, 517)
	for !transfer.Done() {
		started := time.Now()
		if _, err = udp.WriteToUDP(packet, peer); err != nil {
			return
		}
		deadline := time.Now().Add(rto)
		attempts := 0
		for {
			if ctx.Err() != nil {
				return
			}
			if err = udp.SetReadDeadline(deadline); err != nil {
				return
			}
			n, control, source, e := wire.ReadFrom(buffer)
			if e != nil {
				timeout, ok := e.(net.Error)
				if !ok || !timeout.Timeout() || attempts >= 4 {
					return
				}
				packet, err = transfer.Retry()
				if err != nil {
					return
				}
				attempts++
				rto = min(5*time.Second, rto*2)
				deadline = time.Now().Add(rto)
				if _, err = udp.WriteToUDP(packet, peer); err != nil {
					return
				}
				continue
			}
			sender, ok := source.(*net.UDPAddr)
			if !ok || sender.Port != peer.Port || !sender.IP.Equal(peer.IP) || control == nil || control.IfIndex != index || n != 4 {
				continue
			}
			data, e := transfer.Next(buffer[:n])
			if e != nil {
				return
			}
			if data == nil {
				if transfer.Done() {
					return
				}
				continue
			}
			if attempts == 0 {
				rto = max(100*time.Millisecond, min(3*time.Second, (rto*3+time.Since(started)*2)/4))
			}
			packet = data
			break
		}
	}
}
