package taskrunner_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

const projectID = "11111111111111111111111111111111"
const machineID = "22222222222222222222222222222222"

type sshFixture struct {
	root, configFile string
	runner           *taskrunner.SSH
	listener         net.Listener
	runs             atomic.Int64
	closed           atomic.Int64
	drop             atomic.Bool
	tamper           atomic.Bool
	mu               sync.Mutex
	connections      []net.Conn
}

func fixture(t *testing.T) *sshFixture {
	return fixtureWithPublicKey(t, nil)
}

func fixtureWithPublicKey(t *testing.T, authorized ssh.PublicKey) *sshFixture {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	_, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.NewSignerFromKey(private)
	if e != nil {
		t.Fatal(e)
	}
	if authorized == nil {
		authorized = signer.PublicKey()
	}
	_, host, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	hostSigner, _ := ssh.NewSignerFromKey(host)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	f := &sshFixture{root: dir, listener: listener, configFile: filepath.Join(dir, "config.json")}
	key, _ := x509.MarshalPKCS8PrivateKey(private)
	keyfile := filepath.Join(dir, "key.pem")
	if e = os.WriteFile(keyfile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(map[string]any{"targets": []taskrunner.TargetConfig{{ProjectID: projectID, MachineID: machineID, Address: listener.Addr().String(), User: "keel-ctrl", HostKey: ssh.FingerprintSHA256(hostSigner.PublicKey()), CredentialRef: "test-project-rail-key", PrivateKeyFile: keyfile}}})
	if e = os.WriteFile(f.configFile, b, 0600); e != nil {
		t.Fatal(e)
	}
	f.runner, e = taskrunner.Load(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	server := &ssh.ServerConfig{PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if c.User() != "keel-ctrl" || !bytes.Equal(k.Marshal(), authorized.Marshal()) {
			return nil, errors.New("rejected")
		}
		return nil, nil
	}}
	server.AddHostKey(hostSigner)
	go func() {
		for {
			raw, e := listener.Accept()
			if e != nil {
				return
			}
			f.mu.Lock()
			f.connections = append(f.connections, raw)
			f.mu.Unlock()
			go func() {
				conn, channels, requests, e := ssh.NewServerConn(raw, server)
				if e != nil {
					raw.Close()
					return
				}
				defer conn.Close()
				defer f.closed.Add(1)
				go ssh.DiscardRequests(requests)
				for ch := range channels {
					if ch.ChannelType() != "session" {
						ch.Reject(ssh.UnknownChannelType, "session only")
						continue
					}
					channel, requests, e := ch.Accept()
					if e != nil {
						continue
					}
					go func() {
						defer channel.Close()
						for req := range requests {
							if req.Type != "exec" {
								req.Reply(false, nil)
								continue
							}
							var exec struct{ Command string }
							if ssh.Unmarshal(req.Payload, &exec) != nil || exec.Command != "/usr/local/libexec/keel-ssh-runner" {
								req.Reply(false, nil)
								return
							}
							req.Reply(true, nil)
							input, readErr := io.ReadAll(io.LimitReader(channel, 4097))
							var r taskrunner.Request
							if readErr != nil || json.Unmarshal(input, &r) != nil {
								return
							}
							if r.Action == "run" {
								f.runs.Add(1)
							}
							var wire bytes.Buffer
							e := (taskrunner.Wrapper{StateDirectory: dir}).Serve(context.Background(), bytes.NewReader(input), &wire)
							var o taskrunner.Observation
							if e == nil {
								e = json.Unmarshal(wire.Bytes(), &o)
							}
							if f.drop.Load() {
								return
							}
							if f.tamper.Load() {
								o.Command.FencingToken++
							}
							code := uint32(0)
							if e != nil {
								code = 1
							} else {
								json.NewEncoder(channel).Encode(o)
							}
							channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{code}))
							return
						}
					}()
				}
			}()
		}
	}()
	t.Cleanup(func() {
		f.runner.Close()
		listener.Close()
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, c := range f.connections {
			c.Close()
		}
	})
	return f
}
func command() taskrunner.TaskCommand {
	return taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: projectID, MachineID: machineID, CommandID: "33333333333333333333333333333333", AttemptID: "44444444444444444444444444444444", FencingToken: 1, Operation: "host.identity", TimeoutMS: 5000}
}

func TestSSHProtocolPinnedIdentityAndDurableDuplicate(t *testing.T) {
	f := fixture(t)
	c := command()
	ctx := context.Background()
	o, e := f.runner.Do(ctx, taskrunner.Request{Action: "run", Command: c})
	if e != nil || o.State != "success" || o.Output["hostname"] == "" {
		t.Fatalf("identity %v %v", o, e)
	}
	again, e := f.runner.Do(ctx, taskrunner.Request{Action: "run", Command: c})
	if e != nil || !again.StartedAt.Equal(o.StartedAt) || !again.FinishedAt.Equal(o.FinishedAt) {
		t.Fatal("duplicate executed again", e)
	}
	q, e := (taskrunner.Wrapper{StateDirectory: f.root}).Handle(ctx, taskrunner.Request{Action: "query", Command: c})
	if e != nil || !q.FinishedAt.Equal(o.FinishedAt) {
		t.Fatal("fresh wrapper lost receipt", e)
	}
	c.FencingToken++
	if _, e = f.runner.Do(ctx, taskrunner.Request{Action: "run", Command: c}); e == nil {
		t.Fatal("changed command identity accepted")
	}
	b, e := os.ReadFile(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string][]taskrunner.TargetConfig
	json.Unmarshal(b, &cfg)
	cfg["targets"][0].HostKey = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	b, _ = json.Marshal(cfg)
	os.WriteFile(f.configFile, b, 0600)
	wrong, e := taskrunner.Load(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	defer wrong.Close()
	before := f.runs.Load()
	if _, e = wrong.Do(ctx, taskrunner.Request{Action: "run", Command: command()}); e == nil || f.runs.Load() != before {
		t.Fatal("host key mismatch reached wrapper")
	}
}

func TestSSHDisconnectQueriesWithoutReplaying(t *testing.T) {
	f := fixture(t)
	f.drop.Store(true)
	c := command()
	if _, e := f.runner.Do(context.Background(), taskrunner.Request{Action: "run", Command: c}); e == nil {
		t.Fatal("lost response reported success")
	}
	f.drop.Store(false)
	o, e := f.runner.Do(context.Background(), taskrunner.Request{Action: "query", Command: c})
	if e != nil || o.State != "success" || f.runs.Load() != 1 {
		t.Fatal("query replayed or lost persisted receipt", e)
	}
	f.tamper.Store(true)
	if _, e = f.runner.Do(context.Background(), taskrunner.Request{Action: "query", Command: c}); e == nil {
		t.Fatal("wrong fence accepted")
	}
}

func TestSSHCancelNeedsTerminalProof(t *testing.T) {
	f := fixture(t)
	c := command()
	c.Operation = "host.wait"
	c.WaitMS = 10000
	c.TimeoutMS = 15000
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := f.runner.Do(ctx, taskrunner.Request{Action: "run", Command: c}); done <- e }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(f.root, c.CommandID, "receipt.json")); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no receipt")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if e := <-done; e == nil {
		t.Fatal("transport cancellation fabricated terminal proof")
	}
	o, e := f.runner.Do(context.Background(), taskrunner.Request{Action: "cancel", Command: c})
	if e != nil {
		t.Fatal(e)
	}
	for !o.Terminal() {
		if time.Now().After(deadline) {
			t.Fatal("cancel not confirmed")
		}
		time.Sleep(25 * time.Millisecond)
		o, e = f.runner.Do(context.Background(), taskrunner.Request{Action: "query", Command: c})
		if e != nil {
			t.Fatal(e)
		}
	}
	if o.State != "cancelled" || f.runs.Load() != 1 {
		t.Fatal("cancel receipt or replay incorrect", o.State)
	}
}

func TestWrapperRejectsUnsafeInputAndNeverRestartsAccepted(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	w := taskrunner.Wrapper{StateDirectory: dir}
	c := command()
	for _, op := range []string{"sh -c id", "/bin/true", "trust.repair"} {
		bad := c
		bad.Operation = op
		if _, e := w.Handle(context.Background(), taskrunner.Request{Action: "run", Command: bad}); e == nil {
			t.Fatal("unapproved operation accepted")
		}
	}
	b, _ := json.Marshal(taskrunner.Request{Action: "run", Command: c})
	b = append(b, bytes.Repeat([]byte(" "), 4097)...)
	if w.Serve(context.Background(), bytes.NewReader(b), io.Discard) == nil {
		t.Fatal("oversize request accepted")
	}
	path := filepath.Join(dir, c.CommandID)
	os.Mkdir(path, 0700)
	o := taskrunner.Observation{Command: c, State: "accepted", Output: map[string]any{}}
	b, _ = json.Marshal(o)
	os.WriteFile(filepath.Join(path, "receipt.json"), b, 0600)
	result, e := w.Handle(context.Background(), taskrunner.Request{Action: "run", Command: c})
	if e != nil || result.State != "accepted" {
		t.Fatal("ambiguous acceptance replayed", e)
	}
	f := fixture(t)
	os.Chmod(f.configFile, 0644)
	if _, e = taskrunner.Load(f.configFile); e == nil {
		t.Fatal("public config accepted")
	}
}

func TestSSHHandshakeCancellationIsBounded(t *testing.T) {
	f := fixture(t)
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := listener.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	b, _ := os.ReadFile(f.configFile)
	var cfg map[string][]taskrunner.TargetConfig
	json.Unmarshal(b, &cfg)
	cfg["targets"][0].Address = listener.Addr().String()
	b, _ = json.Marshal(cfg)
	os.WriteFile(f.configFile, b, 0600)
	r, e := taskrunner.Load(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, e = r.Do(ctx, taskrunner.Request{Action: "run", Command: command()}); e == nil {
		t.Fatal("stalled handshake accepted")
	}
	if time.Since(started) > time.Second {
		t.Fatal("cancelled handshake exceeded deadline")
	}
	select {
	case c := <-accepted:
		c.Close()
	default:
	}
}
