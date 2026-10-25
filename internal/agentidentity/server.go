// Package agentidentity exposes an opt-in, isolated Agent identity listener.
// It does not register browser APIs, task runners, telemetry or remote commands.
package agentidentity

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/platform"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

type Config struct {
	Listen            string `json:"listen"`
	ServerCertificate string `json:"server_certificate"`
	ServerKey         string `json:"server_key"`
	CACertificate     string `json:"ca_certificate"`
	CAKey             string `json:"ca_key"`
}
type Server struct {
	pb.UnimplementedAgentIdentityServer
	store     *platform.Store
	authority *agentpki.Authority
	slots     chan struct{}
	mu        sync.Mutex
	rates     map[string]rate
}
type rate struct {
	since time.Time
	count int
}

var denied = status.Error(codes.PermissionDenied, "Agent identity or authorization is unavailable")

func Load(path string) (*Config, error) {
	if path == "" {
		return nil, nil
	}
	raw, e := agentpki.ReadFile(path, 8192, true)
	if e != nil {
		return nil, e
	}
	var config Config
	if json.Unmarshal(raw, &config) != nil {
		return nil, agentpki.ErrInvalid
	}
	host, port, e := net.SplitHostPort(config.Listen)
	number, pe := strconv.Atoi(port)
	if e != nil || net.ParseIP(host) == nil || pe != nil || number < 1 || number > 65535 {
		return nil, agentpki.ErrInvalid
	}
	return &config, nil
}
func New(store *platform.Store, config Config) (*grpc.Server, error) {
	if store == nil {
		return nil, agentpki.ErrInvalid
	}
	authority, e := agentpki.Load(config.CACertificate, config.CAKey)
	if e != nil {
		return nil, e
	}
	public, e := agentpki.ReadFile(config.ServerCertificate, 16384, false)
	if e != nil {
		return nil, e
	}
	private, e := agentpki.ReadFile(config.ServerKey, 16384, true)
	if e != nil {
		return nil, e
	}
	pair, e := tls.X509KeyPair(public, private)
	if e != nil {
		return nil, agentpki.ErrInvalid
	}
	leaf, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, agentpki.ErrInvalid
	}
	pool := x509.NewCertPool()
	pool.AddCert(authority.Certificate)
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.VerifyClientCertIfGiven})), grpc.ConnectionTimeout(5*time.Second), grpc.MaxConcurrentStreams(32), grpc.MaxRecvMsgSize(16384), grpc.MaxSendMsgSize(16384), grpc.MaxHeaderListSize(8192))
	pb.RegisterAgentIdentityServer(server, &Server{store: store, authority: authority, slots: make(chan struct{}, 1024), rates: map[string]rate{}})
	return server, nil
}

// Unavailability must close the current stream without turning a temporary
// storage failure into permanent identity rejection on the Agent.
func storeError(err error) error {
	if errors.Is(err, platform.ErrAgentDenied) {
		return denied
	}
	if errors.Is(err, platform.ErrAgentConnectionBusy) {
		return status.Error(codes.ResourceExhausted, "Previous Agent connection lease is still active")
	}
	return status.Error(codes.Unavailable, "Agent identity storage temporarily unavailable")
}

func (s *Server) reserve() bool {
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}
func (s *Server) enrollmentAllowed(ctx context.Context) bool {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return false
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || info.State.Version < tls.VersionTLS13 {
		return false
	}
	host, _, e := net.SplitHostPort(p.Addr.String())
	if e != nil {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.rates[host]
	if now.Sub(r.since) >= time.Minute {
		r = rate{since: now}
	}
	if len(s.rates) >= 4096 {
		for key, value := range s.rates {
			if now.Sub(value.since) >= time.Minute {
				delete(s.rates, key)
			}
		}
		if _, exists := s.rates[host]; !exists && len(s.rates) >= 4096 {
			return false
		}
	}
	r.count++
	s.rates[host] = r
	return r.count <= 10
}
func (s *Server) Enroll(ctx context.Context, in *pb.EnrollRequest) (*pb.EnrollResponse, error) {
	if !s.reserve() {
		return nil, status.Error(codes.ResourceExhausted, "Agent enrollment temporarily unavailable")
	}
	defer func() { <-s.slots }()
	if !s.enrollmentAllowed(ctx) {
		return nil, denied
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, e := s.store.EnrollAgent(work, s.authority, in.GetProjectId(), in.GetMachineId(), in.GetSerialNumber(), in.GetAgentVersion(), in.GetEnrollmentToken(), in.GetCsrPem())
	if e != nil {
		return nil, storeError(e)
	}
	return &pb.EnrollResponse{CredentialId: out.ID, CertificatePem: []byte(out.CertificatePEM), CaCertificatePem: s.authority.PEM, ExpiresAt: out.ExpiresAt.Format(time.RFC3339)}, nil
}
func certificate(ctx context.Context) (*x509.Certificate, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, denied
	}
	info, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(info.State.VerifiedChains) == 0 || len(info.State.PeerCertificates) != 1 {
		return nil, denied
	}
	return info.State.PeerCertificates[0], nil
}
func (s *Server) Heartbeats(stream grpc.BidiStreamingServer[pb.Heartbeat, pb.HeartbeatAck]) error {
	if !s.reserve() {
		return status.Error(codes.ResourceExhausted, "Agent connection capacity reached")
	}
	defer func() { <-s.slots }()
	cert, e := certificate(stream.Context())
	if e != nil {
		return denied
	}
	ctx, cancel := context.WithTimeout(stream.Context(), 2*time.Minute)
	defer cancel()
	type received struct {
		message *pb.Heartbeat
		err     error
	}
	messages := make(chan received, 1)
	go func() {
		for {
			message, err := stream.Recv()
			select {
			case messages <- received{message, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	idle := time.NewTimer(20 * time.Second)
	defer idle.Stop()
	check := time.NewTicker(2 * time.Second)
	defer check.Stop()
	var connection string
	var first *pb.Heartbeat
	var sequence uint64
	defer func() {
		if connection != "" {
			work, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			_ = s.store.CloseAgentConnection(work, connection)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.DeadlineExceeded, "Agent connection window ended")
		case <-idle.C:
			return status.Error(codes.DeadlineExceeded, "Agent heartbeat deadline exceeded")
		case <-check.C:
			work, stop := context.WithTimeout(ctx, 3*time.Second)
			_, err := s.store.CheckAgentCertificate(work, cert, s.authority.ID)
			stop()
			if err != nil {
				return storeError(err)
			}
		case incoming := <-messages:
			if incoming.err != nil {
				if errors.Is(incoming.err, io.EOF) {
					return nil
				}
				return denied
			}
			in := incoming.message
			if in.GetSequence() != sequence+1 {
				return denied
			}
			work, stop := context.WithTimeout(ctx, 5*time.Second)
			if first == nil {
				connection, e = s.store.OpenAgentConnection(work, cert, s.authority.ID, in.GetProjectId(), in.GetMachineId(), in.GetSerialNumber(), in.GetAgentVersion(), in.GetBootId())
				if e == nil {
					first = in
				}
			}
			if e == nil && (first.GetProjectId() != in.GetProjectId() || first.GetMachineId() != in.GetMachineId() || first.GetSerialNumber() != in.GetSerialNumber() || first.GetAgentVersion() != in.GetAgentVersion() || first.GetBootId() != in.GetBootId()) {
				e = platform.ErrAgentDenied
			}
			if e == nil {
				e = s.store.AgentHeartbeat(work, cert, s.authority.ID, connection, in.GetSequence())
			}
			stop()
			if e != nil {
				return storeError(e)
			}
			sequence = in.GetSequence()
			sent := make(chan error, 1)
			go func() {
				sent <- stream.Send(&pb.HeartbeatAck{ConnectionId: connection, Sequence: sequence, ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano), NextHeartbeatSeconds: 10})
			}()
			select {
			case e = <-sent:
				if e != nil {
					return denied
				}
			case <-ctx.Done():
				return status.Error(codes.DeadlineExceeded, "Agent connection window ended")
			}
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(20 * time.Second)
		}
	}
}
