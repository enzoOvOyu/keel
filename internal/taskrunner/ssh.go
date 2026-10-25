package taskrunner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

type TargetConfig struct {
	ProjectID      string `json:"project_id"`
	MachineID      string `json:"machine_id"`
	Address        string `json:"address"`
	User           string `json:"user"`
	HostKey        string `json:"host_key"`
	CredentialRef  string `json:"credential_ref"`
	PrivateKeyFile string `json:"private_key_file"`
}
type config struct {
	Targets []TargetConfig `json:"targets"`
}
type endpoint struct {
	config  TargetConfig
	signer  ssh.Signer
	binding Binding
}
type SSH struct {
	mu      sync.Mutex
	targets map[string]endpoint
	pool    map[string]*ssh.Client
	closed  bool
}

func readPrivate(path string, max int64) ([]byte, error) {
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrRejected
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Uid != uint32(os.Geteuid()) || st.Size > max {
		return nil, ErrRejected
	}
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, ErrRejected
	}
	return b, nil
}

// Load is opt-in. The file is an operator-owned project/machine allowlist;
// secrets and addresses cannot be supplied by an execution request.
func Load(path string) (*SSH, error) {
	if path == "" {
		return nil, nil
	}
	b, e := readPrivate(path, 128*1024)
	if e != nil {
		return nil, ErrRejected
	}
	var c config
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return nil, ErrRejected
	}
	var extra any
	if d.Decode(&extra) != io.EOF || len(c.Targets) == 0 || len(c.Targets) > 128 {
		return nil, ErrRejected
	}
	s := &SSH{targets: map[string]endpoint{}, pool: map[string]*ssh.Client{}}
	for _, t := range c.Targets {
		ip, port, e := net.SplitHostPort(t.Address)
		n, pe := strconv.Atoi(port)
		// Explicit IPs prevent DNS changes silently retargeting a queued command.
		if !idPattern.MatchString(t.ProjectID) || !idPattern.MatchString(t.MachineID) || e != nil || pe != nil || n < 1 || n > 65535 || net.ParseIP(ip) == nil || net.ParseIP(ip).IsUnspecified() || net.ParseIP(ip).IsMulticast() || t.User != "keel-ctrl" || len(t.CredentialRef) < 1 || len(t.CredentialRef) > 64 || strings.TrimSpace(t.CredentialRef) != t.CredentialRef || !strings.HasPrefix(t.HostKey, "SHA256:") || len(t.HostKey) != 50 {
			return nil, ErrRejected
		}
		pin, err := base64.RawStdEncoding.DecodeString(t.HostKey[7:])
		if err != nil || len(pin) != 32 || base64.RawStdEncoding.EncodeToString(pin) != t.HostKey[7:] {
			return nil, ErrRejected
		}
		key, e := readPrivate(t.PrivateKeyFile, 64*1024)
		if e != nil {
			return nil, ErrRejected
		}
		private, e := ssh.ParseRawPrivateKey(key)
		for i := range key {
			key[i] = 0
		}
		if e != nil {
			return nil, ErrRejected
		}
		if _, ok := private.(ed25519.PrivateKey); !ok {
			return nil, ErrRejected
		}
		signer, e := ssh.NewSignerFromKey(private)
		if e != nil {
			return nil, ErrRejected
		}
		// Include public credential identity: rotation invalidates old snapshots.
		identity := struct {
			Target    TargetConfig
			PublicKey string
		}{t, ssh.FingerprintSHA256(signer.PublicKey())}
		identity.Target.PrivateKeyFile = ""
		raw, _ := json.Marshal(identity)
		hash := sha256.Sum256(raw)
		ep := endpoint{t, signer, Binding{t.HostKey, hex.EncodeToString(hash[:])}}
		id := t.ProjectID + ":" + t.MachineID
		if _, ok := s.targets[id]; ok {
			return nil, ErrRejected
		}
		s.targets[id] = ep
	}
	return s, nil
}

func (s *SSH) Binding(project, machine string) (Binding, error) {
	if s == nil {
		return Binding{}, ErrRejected
	}
	e, ok := s.targets[project+":"+machine]
	if !ok {
		return Binding{}, ErrRejected
	}
	return e.binding, nil
}
func (s *SSH) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, c := range s.pool {
		c.Close()
	}
	s.pool = map[string]*ssh.Client{}
}
func (s *SSH) connection(ctx context.Context, id string, ep endpoint) (*ssh.Client, error) {
	if ctx.Err() != nil {
		return nil, ErrUnknown
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrRejected
	}
	if c := s.pool[id]; c != nil {
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock() // A slow target must not serialize unrelated handshakes.
	config := &ssh.ClientConfig{User: ep.config.User, Auth: []ssh.AuthMethod{ssh.PublicKeys(ep.signer)}, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if ssh.FingerprintSHA256(k) != ep.binding.HostKey {
			return ErrRejected
		}
		return nil
	}}
	dial := net.Dialer{Timeout: 5 * time.Second}
	conn, e := dial.DialContext(ctx, "tcp", ep.config.Address)
	if e != nil {
		return nil, ErrUnknown
	}
	deadline := time.Now().Add(5 * time.Second)
	if t, ok := ctx.Deadline(); ok && t.Before(deadline) {
		deadline = t
	}
	conn.SetDeadline(deadline)
	stopCancellation := context.AfterFunc(ctx, func() { conn.Close() })
	cc, ch, req, e := ssh.NewClientConn(conn, ep.config.Address, config)
	if !stopCancellation() {
		conn.Close()
		return nil, ErrUnknown
	}
	if e != nil {
		conn.Close()
		return nil, ErrUnknown
	}
	conn.SetDeadline(time.Time{})
	c := ssh.NewClient(cc, ch, req)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		c.Close()
		return nil, ErrRejected
	}
	if prior := s.pool[id]; prior != nil {
		c.Close()
		return prior, nil
	}
	s.pool[id] = c
	return c, nil
}
func (s *SSH) discard(id string, c *ssh.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pool[id] == c {
		delete(s.pool, id)
	}
	c.Close()
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16384 {
		return 0, ErrRejected
	}
	return b.Buffer.Write(p)
}

func (s *SSH) Do(ctx context.Context, r Request) (Observation, error) {
	var zero Observation
	if s == nil || r.Command.Validate() != nil || (r.Action != "run" && r.Action != "query" && r.Action != "cancel") {
		return zero, ErrRejected
	}
	id := r.Command.ProjectID + ":" + r.Command.MachineID
	ep, ok := s.targets[id]
	if !ok {
		return zero, ErrRejected
	}
	work, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	c, e := s.connection(work, id, ep)
	if e != nil {
		return zero, e
	}
	// All SSH operations, including opening a session, are bounded. A transport
	// error never creates a stopped receipt or causes an implicit replay.
	type response struct {
		o Observation
		e error
	}
	done := make(chan response, 1)
	go func() {
		if work.Err() != nil {
			done <- response{e: ErrUnknown}
			return
		}
		session, e := c.NewSession()
		if e != nil {
			done <- response{e: ErrUnknown}
			return
		}
		defer session.Close()
		if work.Err() != nil {
			done <- response{e: ErrUnknown}
			return
		}
		input, _ := json.Marshal(r)
		session.Stdin = bytes.NewReader(input)
		var out limitedOutput
		session.Stdout = &out
		session.Stderr = io.Discard
		// Constant operator-installed entrypoint; data travels only over stdin.
		if e = session.Run("/usr/local/libexec/keel-ssh-runner"); e != nil {
			done <- response{e: ErrUnknown}
			return
		}
		var o Observation
		d := json.NewDecoder(bytes.NewReader(out.Bytes()))
		d.DisallowUnknownFields()
		if d.Decode(&o) != nil {
			done <- response{e: ErrUnknown}
			return
		}
		var extra any
		if d.Decode(&extra) != io.EOF || o.Validate(r.Command) != nil {
			done <- response{e: ErrUnknown}
			return
		}
		done <- response{o: o}
	}()
	select {
	case <-work.Done():
		s.discard(id, c)
		return zero, ErrUnknown
	case v := <-done:
		if v.e != nil {
			s.discard(id, c)
		}
		return v.o, v.e
	}
}

var _ Runner = (*SSH)(nil)
