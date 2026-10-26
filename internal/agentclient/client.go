// Package agentclient implements enrollment, authenticated liveness and opt-in bounded tasks.
package agentclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
	"github.com/enzoOvOyu/keel/internal/agentpki"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type Config struct {
	ArtifactTransfersEnabled bool   `json:"artifact_transfers_enabled"`
	MediaCacheEnabled        bool   `json:"media_cache_enabled"`
	CacheInventoryEnabled    bool   `json:"cache_inventory_enabled"`
	NetworkInventoryEnabled  bool   `json:"network_inventory_enabled"`
	RenewalEnabled           bool   `json:"renewal_enabled"`
	TasksEnabled             bool   `json:"tasks_enabled"`
	ScriptsEnabled           bool   `json:"scripts_enabled"`
	Endpoint                 string `json:"endpoint"`
	ServerName               string `json:"server_name"`
	ServerCA                 string `json:"server_ca"`
	AgentCA                  string `json:"agent_ca"`
	ProjectID                string `json:"project_id"`
	MachineID                string `json:"machine_id"`
	Serial                   string `json:"serial_number"`
	StateDirectory           string `json:"state_directory"`
	TokenFile                string `json:"enrollment_token_file"`
}
type Identity struct {
	CredentialID string `json:"credential_id"`
	ProjectID    string `json:"project_id"`
	MachineID    string `json:"machine_id"`
	Serial       string `json:"serial_number"`
	Version      string `json:"agent_version"`
	Certificate  string `json:"certificate_pem"`
}

var identityID = regexp.MustCompile(`^[0-9a-f]{32}$`)

var ErrIdentity = errors.New("Agent configuration or identity unavailable; operator action required")

func Load(path string) (Config, error) {
	var c Config
	raw, e := agentpki.ReadFile(path, 8192, true)
	if e != nil {
		return c, ErrIdentity
	}
	if json.Unmarshal(raw, &c) != nil {
		return c, ErrIdentity
	}
	return c, c.Validate()
}
func (c Config) Validate() error {
	if c.ArtifactTransfersEnabled && !c.TasksEnabled || c.MediaCacheEnabled && !c.ScriptsEnabled || c.CacheInventoryEnabled && !c.TasksEnabled || c.ScriptsEnabled && !c.TasksEnabled || (c.TasksEnabled || c.NetworkInventoryEnabled) && os.Geteuid() == 0 {
		return ErrIdentity
	}
	host, port, e := net.SplitHostPort(c.Endpoint)
	number, pe := strconv.Atoi(port)
	if e != nil || pe != nil || number < 1 || number > 65535 || host == "" || c.ServerName == "" || strings.ContainsAny(c.Endpoint+c.ServerName, "\x00\r\n/") || !filepath.IsAbs(c.StateDirectory) {
		return ErrIdentity
	}
	info, e := os.Lstat(c.StateDirectory)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrIdentity
	}
	if !identityID.MatchString(c.ProjectID) || !identityID.MatchString(c.MachineID) || len(c.Serial) < 1 || len(c.Serial) > 128 || strings.ContainsAny(c.Serial, "\x00\r\n") {
		return ErrIdentity
	}
	return nil
}
func (c Config) tls(certificates []tls.Certificate) (*tls.Config, error) {
	raw, e := agentpki.ReadFile(c.ServerCA, 16384, false)
	if e != nil {
		return nil, ErrIdentity
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(raw) {
		return nil, ErrIdentity
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: c.ServerName, Certificates: certificates}, nil
}
func (c Config) connect(certificates []tls.Certificate) (*grpc.ClientConn, error) {
	config, e := c.tls(certificates)
	if e != nil {
		return nil, e
	}
	return grpc.NewClient("passthrough:///"+c.Endpoint, grpc.WithTransportCredentials(credentials.NewTLS(config)), grpc.WithNoProxy(), grpc.WithDisableRetry(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(16384), grpc.MaxCallSendMsgSize(32768)))
}
func (c Config) verify(identity Identity, key []byte) error {
	if identity.ProjectID != c.ProjectID || identity.MachineID != c.MachineID || identity.Serial != c.Serial {
		return ErrIdentity
	}
	pair, e := tls.X509KeyPair([]byte(identity.Certificate), key)
	if e != nil {
		return ErrIdentity
	}
	cert, e := x509.ParseCertificate(pair.Certificate[0])
	if e != nil || len(cert.URIs) != 1 || cert.URIs[0].String() != agentpki.URI(c.ProjectID, c.MachineID, identity.CredentialID).String() {
		return ErrIdentity
	}
	ca, e := agentpki.ReadFile(c.AgentCA, 16384, false)
	if e != nil {
		return ErrIdentity
	}
	root, e := agentpki.Certificate(ca)
	if e != nil {
		return ErrIdentity
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	if _, e = cert.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); e != nil {
		return ErrIdentity
	}
	return nil
}

// Enroll never automatically retries. If the response is lost after issuance,
// the pending private key remains and another enrollment needs owner review.
func Enroll(ctx context.Context, c Config, version string) (Identity, error) {
	var identity Identity
	if c.Validate() != nil {
		return identity, ErrIdentity
	}
	if _, e := os.Lstat(filepath.Join(c.StateDirectory, "identity.json")); !os.IsNotExist(e) {
		return identity, ErrIdentity
	}
	token, e := agentpki.ReadFile(c.TokenFile, 256, true)
	if e != nil {
		return identity, ErrIdentity
	}
	_, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return identity, ErrIdentity
	}
	keyDER, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return identity, ErrIdentity
	}
	private := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if e = agentpki.WriteNew(filepath.Join(c.StateDirectory, "agent-key.pem"), private); e != nil {
		return identity, ErrIdentity
	}
	csr, e := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if e != nil {
		return identity, ErrIdentity
	}
	connection, e := c.connect(nil)
	if e != nil {
		return identity, ErrIdentity
	}
	defer connection.Close()
	work, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	response, e := pb.NewAgentIdentityClient(connection).Enroll(work, &pb.EnrollRequest{ProjectId: c.ProjectID, MachineId: c.MachineID, SerialNumber: c.Serial, AgentVersion: version, EnrollmentToken: strings.TrimSpace(string(token)), CsrPem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})})
	if e != nil {
		return identity, ErrIdentity
	}
	identity = Identity{CredentialID: response.GetCredentialId(), ProjectID: c.ProjectID, MachineID: c.MachineID, Serial: c.Serial, Version: version, Certificate: string(response.GetCertificatePem())}
	if c.verify(identity, private) != nil {
		return Identity{}, ErrIdentity
	}
	raw, e := json.Marshal(identity)
	if e != nil || agentpki.WriteNew(filepath.Join(c.StateDirectory, "identity.json"), raw) != nil {
		return Identity{}, ErrIdentity
	}
	return identity, nil
}
func Stream(ctx context.Context, c Config, version string, observe func(*pb.HeartbeatAck)) (result error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	taskErrors := make(chan error, 1)
	taskDone := make(chan struct{})
	tasksStarted := false
	defer func() {
		if tasksStarted {
			// Preserve the cause that ended the streams. Our own shutdown cancellation
			// must not mask a real heartbeat authorization or storage failure.
			select {
			case err := <-taskErrors:
				if err != nil {
					result = err
				}
			default:
			}
		}
		cancel()
		if tasksStarted {
			<-taskDone
		}
	}()
	if c.Validate() != nil {
		return ErrIdentity
	}
	raw, e := agentpki.ReadFile(filepath.Join(c.StateDirectory, "identity.json"), 16384, true)
	if e != nil {
		return ErrIdentity
	}
	var identity Identity
	if json.Unmarshal(raw, &identity) != nil || identity.Version != version {
		return ErrIdentity
	}
	key, e := agentpki.ReadFile(filepath.Join(c.StateDirectory, "agent-key.pem"), 16384, true)
	if e != nil || c.verify(identity, key) != nil {
		return ErrIdentity
	}
	pair, e := tls.X509KeyPair([]byte(identity.Certificate), key)
	if e != nil {
		return ErrIdentity
	}
	connection, e := c.connect([]tls.Certificate{pair})
	if e != nil {
		return ErrIdentity
	}
	defer connection.Close()
	stream, e := pb.NewAgentIdentityClient(connection).Heartbeats(ctx)
	if e != nil {
		return e
	}
	defer stream.CloseSend()
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	bootID := strings.ReplaceAll(strings.TrimSpace(string(boot)), "-", "")
	if e != nil || !identityID.MatchString(bootID) {
		return ErrIdentity
	}
	var networkBaseline []agentinventory.Interface
	for sequence := uint64(1); ; sequence++ {
		message := &pb.Heartbeat{ProjectId: c.ProjectID, MachineId: c.MachineID, SerialNumber: c.Serial, AgentVersion: version, BootId: bootID, Sequence: sequence}
		nextBaseline := networkBaseline
		if c.NetworkInventoryEnabled {
			frame, next := networkHeartbeat(networkBaseline, time.Now())
			raw, err := json.Marshal(frame)
			if err != nil {
				return ErrIdentity
			}
			if len(raw) > agentinventory.MaxNetworkFrameBytes {
				frame = agentinventory.NetworkFrame{Version: agentinventory.NetworkVersion, State: "unavailable", CapturedAt: time.Now().UTC(), Interfaces: []agentinventory.Interface{}, Removed: []int{}, FailureCode: "inventory_frame_limit"}
				raw, err = json.Marshal(frame)
				if err != nil {
					return ErrIdentity
				}
			} else {
				nextBaseline = next
			}
			message.NetworkInventoryJson = raw
		}
		if c.CacheInventoryEnabled {
			frame := taskrunner.CollectMediaCacheIndex(ctx, time.Now())
			raw, err := json.Marshal(frame)
			if err != nil || len(raw) > agentinventory.MaxCacheFrameBytes {
				return ErrIdentity
			}
			message.CacheInventoryJson = raw
		}
		if e = stream.Send(message); e != nil {
			return e
		}
		ack, e := stream.Recv()
		if e != nil {
			return e
		}
		if ack.GetSequence() != sequence || !identityID.MatchString(ack.GetConnectionId()) || ack.GetNextHeartbeatSeconds() < 1 || ack.GetNextHeartbeatSeconds() > 10 {
			return ErrIdentity
		}
		networkBaseline = nextBaseline
		if c.TasksEnabled && !tasksStarted {
			tasksStarted = true
			go func(connectionID string) {
				defer close(taskDone)
				err := c.taskStream(ctx, pair, connectionID)
				taskErrors <- err
				cancel()
			}(ack.GetConnectionId())
		}
		if observe != nil {
			observe(ack)
		}
		timer := time.NewTimer(time.Duration(ack.GetNextHeartbeatSeconds()) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
